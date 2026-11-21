package userbot

import (
	"assterisk/ubot/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func canonicalGroupID(peerType string, rawID int64) int64 {
	if rawID == 0 {
		return 0
	}
	if rawID < 0 {
		s := strconv.FormatInt(rawID, 10)
		if strings.HasPrefix(s, "-100") {
			return rawID
		}
		if peerType == "channel" {
			if parsed, err := strconv.ParseInt("-100"+strconv.FormatInt(-rawID, 10), 10, 64); err == nil {
				return parsed
			}
		}
		return rawID
	}
	if peerType == "channel" {
		if parsed, err := strconv.ParseInt("-100"+strconv.FormatInt(rawID, 10), 10, 64); err == nil {
			return parsed
		}
	}
	return -rawID
}

type manualGroupOutcome struct {
	Target string
	Group  *jasebGroupDoc
	Status string
	ErrMsg string
}

func parseManualGroupTokens(rawText string) []string {
	lines := strings.Split(strings.ReplaceAll(rawText, "\r\n", "\n"), "\n")
	seen := make(map[string]bool)
	var out []string
	for _, line := range lines {
		for _, field := range strings.Fields(line) {
			tok := strings.TrimSpace(strings.Trim(field, ",;"))
			if tok == "" {
				continue
			}
			key := strings.ToLower(tok)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, tok)
		}
	}
	return out
}

func (s *Service) handleAddGroupsManual(ctx context.Context, client *gobot.Client, job models.PToService, ownerID int64) {
	replyState := "panel_notice"
	if job.ChatID != 0 && ownerID != 0 && job.ChatID != ownerID {
		replyState = "remote_panel_notice"
	}

	if client == nil {
		s.reply(ctx, job.ChatID, job.ThreadID, `<h1>⚠️ Sesi Tidak Aktif</h1><blockquote expandable>Akun userbot belum aktif. Hubungkan ulang sesi terlebih dahulu.</blockquote>`, replyState)
		return
	}

	tokens := parseManualGroupTokens(job.Text)
	if len(tokens) == 0 {
		s.reply(ctx, job.ChatID, job.ThreadID, `<h1>⚠️ Target Kosong</h1><blockquote expandable>Tidak ada target grup yang valid ditemukan.</blockquote>`, replyState)
		return
	}

	hasNumeric := false
	for _, tok := range tokens {
		if isManualNumericOrChannelLink(tok) {
			hasNumeric = true
			break
		}
	}

	var cachedChannels map[int64]*tg.Channel
	var cachedChats map[int64]*tg.Chat
	if hasNumeric {
		cachedChannels, cachedChats = s.scanJoinedGroupIndex(ctx, client)
	}

	outcomes := make([]manualGroupOutcome, 0, len(tokens))
	resolvedGroups := make([]jasebGroupDoc, 0, len(tokens))
	for i, tok := range tokens {
		if i > 0 {
			select {
			case <-ctx.Done():
				break
			case <-time.After(600 * time.Millisecond):
			}
		}
		out := s.resolveAndJoinManualGroupToken(ctx, client, tok, cachedChannels, cachedChats)
		outcomes = append(outcomes, out)
		if out.Group != nil {
			resolvedGroups = append(resolvedGroups, *out.Group)
		}
	}

	addedCount := 0
	updatedCount := 0
	if len(resolvedGroups) > 0 {
		addedCount, updatedCount = s.upsertManualJasebGroups(ctx, ownerID, resolvedGroups)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<h1>📥 Hasil Tambah Grup Manual</h1><hr/>`)
	fmt.Fprintf(&b, `<b>📊 Ringkasan:</b> Ditambahkan <b>%d</b>, Diperbarui <b>%d</b>, Total diproses <b>%d</b><br/><blockquote expandable>`, addedCount, updatedCount, len(outcomes))
	for i, out := range outcomes {
		if i > 0 {
			b.WriteString("\n")
		}
		if out.ErrMsg != "" {
			fmt.Fprintf(&b, "%d. ❌ <code>%s</code> — %s", i+1, html.EscapeString(out.Target), html.EscapeString(out.ErrMsg))
			continue
		}
		title := out.Group.Title
		if title == "" {
			title = out.Group.Target
		}
		fmt.Fprintf(&b, "%d. ✅ <b>%s</b> (<code>%s</code>) — %s", i+1, html.EscapeString(title), html.EscapeString(out.Group.Target), html.EscapeString(out.Status))
	}
	b.WriteString(`</blockquote><hr/><p>GotdBot • Manual Group Manager</p>`)

	syncedJSON, _ := json.Marshal(resolvedGroups)
	if job.ChatID != 0 {
		_ = s.redis.To(ctx, "from:ubot", &models.PFromService{
			ChatID:   job.ChatID,
			ThreadID: job.ThreadID,
			OwnerID:  ownerID,
			Type:     "manual_groups_synced",
			TaskID:   string(syncedJSON),
			State:    replyState,
			Text:     b.String(),
		})
	}
}

func isManualNumericOrChannelLink(tok string) bool {
	if _, ok := extractTelegramCChannelID(tok); ok {
		return true
	}
	_, err := strconv.ParseInt(tok, 10, 64)
	return err == nil
}

func (s *Service) scanJoinedGroupIndex(ctx context.Context, client *gobot.Client) (map[int64]*tg.Channel, map[int64]*tg.Chat) {
	chans := make(map[int64]*tg.Channel)
	chats := make(map[int64]*tg.Chat)
	if client == nil {
		return chans, chats
	}
	scanCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	_ = dialogs.NewQueryBuilder(client.API()).GetDialogs().BatchSize(100).ForEach(scanCtx, func(_ context.Context, elem dialogs.Elem) error {
		for _, ch := range elem.Entities.Channels() {
			if ch != nil {
				chans[ch.ID] = ch
			}
		}
		for _, c := range elem.Entities.Chats() {
			if c != nil {
				chats[c.ID] = c
			}
		}
		return nil
	})
	return chans, chats
}

func groupDocFromJoinResult(res tg.MessagesChatInviteJoinResultClass) (*jasebGroupDoc, bool) {
	switch v := res.(type) {
	case *tg.MessagesChatInviteJoinResultOk:
		return groupDocFromUpdates(v.Updates), false
	case *tg.MessagesChatInviteJoinResultWebView:
		return nil, true
	}
	return nil, false
}

func groupDocFromUpdates(updates tg.UpdatesClass) *jasebGroupDoc {
	var chats []tg.ChatClass
	switch u := updates.(type) {
	case *tg.Updates:
		chats = u.Chats
	case *tg.UpdatesCombined:
		chats = u.Chats
	}
	for _, c := range chats {
		if doc := groupDocFromChatClass(c); doc != nil {
			return doc
		}
	}
	return nil
}

func groupDocFromChatClass(c tg.ChatClass) *jasebGroupDoc {
	switch v := c.(type) {
	case *tg.Channel:
		canon := canonicalGroupID("channel", v.ID)
		return &jasebGroupDoc{
			Target:     strconv.FormatInt(canon, 10),
			Title:      strings.TrimSpace(v.Title),
			ChatID:     canon,
			AccessHash: v.AccessHash,
			PeerType:   "channel",
			IsForum:    v.Forum,
		}
	case *tg.Chat:
		canon := canonicalGroupID("chat", v.ID)
		return &jasebGroupDoc{
			Target:   strconv.FormatInt(canon, 10),
			Title:    strings.TrimSpace(v.Title),
			ChatID:   canon,
			PeerType: "chat",
		}
	}
	return nil
}

func (s *Service) upsertManualJasebGroups(ctx context.Context, ownerID int64, incoming []jasebGroupDoc) (int, int) {
	cfg := s.getJasebConfig(ctx, ownerID)
	groups := append([]jasebGroupDoc(nil), cfg.Groups...)

	added := 0
	updated := 0
	for _, item := range incoming {
		matchedIdx := -1
		for i, existing := range groups {
			if (existing.Target != "" && existing.Target == item.Target) || sameGroupID(existing.ChatID, item.ChatID) {
				matchedIdx = i
				break
			}
		}
		if matchedIdx >= 0 {
			cur := &groups[matchedIdx]
			changed := false
			if item.Title != "" && (cur.Title == "" || cur.Title == cur.Target || cur.Title != item.Title) && !strings.HasPrefix(item.Title, "Grup ") {
				cur.Title = item.Title
				changed = true
			}
			if item.Target != "" && cur.Target != item.Target && !strings.HasPrefix(cur.Target, "@") {
				cur.Target = item.Target
				changed = true
			}
			if item.ChatID != 0 && cur.ChatID != item.ChatID {
				cur.ChatID = item.ChatID
				changed = true
			}
			if item.AccessHash != 0 && cur.AccessHash == 0 {
				cur.AccessHash = item.AccessHash
				changed = true
			}
			if item.PeerType != "" && cur.PeerType == "" {
				cur.PeerType = item.PeerType
				changed = true
			}
			if item.IsForum && !cur.IsForum {
				cur.IsForum = true
				changed = true
			}
			if changed {
				updated++
			}
			continue
		}
		groups = append(groups, item)
		added++
	}

	if added == 0 && updated == 0 {
		return 0, 0
	}

	if s.db != nil {
		_, _ = s.db.Collection("jaseb_configurasi").UpdateOne(
			ctx,
			bson.M{"_id": ownerID},
			bson.M{"$set": bson.M{"groups": groups, "updated_at": time.Now()}},
			options.Update().SetUpsert(true),
		)
		s.publishConfigUpdated(ctx, ownerID)
	}
	if s.sqlite != nil {
		groupsJSON, _ := json.Marshal(groups)
		_, _ = s.sqlite.DB.ExecContext(
			ctx,
			`INSERT INTO jaseb_configurasi (owner_id, groups, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(owner_id) DO UPDATE SET groups = excluded.groups, updated_at = CURRENT_TIMESTAMP;`,
			ownerID, string(groupsJSON),
		)
	}
	s.invalidateConfigCache(ctx, ownerID, true)
	return added, updated
}
