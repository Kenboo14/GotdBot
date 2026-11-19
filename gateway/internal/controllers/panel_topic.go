package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type topicForumGroup struct {
	ChatID     int64  `json:"chat_id"`
	Title      string `json:"title"`
	Target     string `json:"target"`
	AccessHash int64  `json:"access_hash,omitempty"`
}

type topicForumItem struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Closed bool   `json:"closed,omitempty"`
}

type topicForumResponse struct {
	ChatID     int64            `json:"chat_id"`
	GroupTitle string           `json:"group_title"`
	Topics     []topicForumItem `json:"topics"`
}

type topicSessionState struct {
	ForumGroups    []topicForumGroup
	SelectedGroup  topicForumGroup
	Topics         []topicForumItem
	PendingLinks   []databases.TopicLink
	LinkTargetList string
	PromptMsgID    int
	ListMsgID      int
	LastForumScan  time.Time
}

var topicListNameRegex = regexp.MustCompile(`^[a-z0-9._-]{1,24}$`)

func (h *Handler) ensureTopicSessionLocked(chatID int64) *topicSessionState {
	if h.topicSessions == nil {
		h.topicSessions = make(map[int64]*topicSessionState)
	}
	sess := h.topicSessions[chatID]
	if sess == nil {
		sess = &topicSessionState{}
		h.topicSessions[chatID] = sess
	}
	return sess
}

func (h *Handler) getTopicSession(chatID int64) *topicSessionState {
	h.topicMu.Lock()
	defer h.topicMu.Unlock()
	return h.ensureTopicSessionLocked(chatID)
}

func (h *Handler) setTopicListMsgID(chatID int64, msgID int) {
	if chatID == 0 || msgID <= 0 {
		return
	}
	h.topicMu.Lock()
	h.ensureTopicSessionLocked(chatID).ListMsgID = msgID
	h.topicMu.Unlock()
}

func (h *Handler) setTopicPromptMsgID(chatID int64, msgID int) {
	if chatID == 0 {
		return
	}
	h.topicMu.Lock()
	h.ensureTopicSessionLocked(chatID).PromptMsgID = msgID
	h.topicMu.Unlock()
}

func (h *Handler) getTopicListEditID(chatID int64) int {
	h.topicMu.Lock()
	defer h.topicMu.Unlock()
	sess := h.ensureTopicSessionLocked(chatID)
	if sess.PromptMsgID > 0 {
		return sess.PromptMsgID
	}
	return sess.ListMsgID
}

func (h *Handler) getTopicLastForumScan(chatID int64) time.Time {
	h.topicMu.Lock()
	defer h.topicMu.Unlock()
	return h.ensureTopicSessionLocked(chatID).LastForumScan
}

func (h *Handler) setTopicLastForumScan(chatID int64, ts time.Time) {
	h.topicMu.Lock()
	h.ensureTopicSessionLocked(chatID).LastForumScan = ts
	h.topicMu.Unlock()
}

func (h *Handler) resetTopicForumScan(chatID int64) {
	h.topicMu.Lock()
	h.ensureTopicSessionLocked(chatID).LastForumScan = time.Time{}
	h.topicMu.Unlock()
}

func (h *Handler) deleteOtherDetailMessages(ctx context.Context, b *botgotd.Bot, chatID int64, keepMsgID int) {
	ids := h.clearAndGetDetailMessageIDs(ctx, chatID)
	toDelete := make([]int, 0, len(ids))
	for _, id := range ids {
		if id > 0 && id != keepMsgID {
			toDelete = append(toDelete, id)
		}
	}
	if len(toDelete) > 0 {
		h.deleteBotMessages(ctx, b, chatID, toDelete)
	}
}

func (h *Handler) cachedForumGroupsFromDB(ctx context.Context, chatID, userID int64) ([]topicForumGroup, bool, bool, int) {
	if h.db == nil {
		return nil, false, false, 0
	}
	cfg := h.db.GetUserConfig(ctx, userID)
	if len(cfg.Groups) == 0 {
		if refreshed := h.db.SyncOneFromMongoDB(ctx, userID); refreshed != nil {
			cfg = refreshed
		}
	}
	totalGroups := len(cfg.Groups)
	forums := make([]topicForumGroup, 0)
	hasUnprobed := false
	for _, g := range cfg.Groups {
		if g.IsForum {
			title := strings.TrimSpace(g.Title)
			if title == "" {
				title = g.Target
			}
			forums = append(forums, topicForumGroup{
				ChatID:     g.ChatID,
				Title:      title,
				Target:     g.Target,
				AccessHash: g.AccessHash,
			})
		} else if g.AccessHash == 0 && g.PeerType != "chat" {
			hasUnprobed = true
		}
	}
	lastScan := h.getTopicLastForumScan(chatID)
	useFastPath := len(forums) > 0 && (!hasUnprobed || (!lastScan.IsZero() && time.Since(lastScan) < 5*time.Minute))
	wantsBgRefresh := useFastPath && (lastScan.IsZero() || time.Since(lastScan) >= 5*time.Minute)
	return forums, useFastPath, wantsBgRefresh, totalGroups
}

func (h *Handler) triggerBgForumRefresh(ctx context.Context, chatID, userID int64) {
	if h.broker == nil {
		return
	}
	_ = h.broker.To(ctx, "to:ubot:command", models.PToService{
		ChatID:  chatID,
		UserID:  userID,
		OwnerID: userID,
		Type:    "fetch_forum_groups",
		TaskID:  "bg_refresh_forum",
	})
}

func topicGroupReplyMarkup(count int) *botgotd.ReplyKeyboardMarkup {
	rows := make([][]botgotd.KeyboardButton, 0, (count/5)+2)
	for start := 0; start < count; start += 5 {
		row := make([]botgotd.KeyboardButton, 0, 5)
		for i := start; i < start+5 && i < count; i++ {
			row = append(row, botgotd.KeyboardButton{Text: strconv.Itoa(i + 1)})
		}
		rows = append(rows, row)
	}
	rows = append(rows, []botgotd.KeyboardButton{{Text: assets.BtnBatal}})
	return &botgotd.ReplyKeyboardMarkup{Keyboard: rows, ResizeKeyboard: true}
}

func topicSelectReplyMarkup(count int, includeActions bool) *botgotd.ReplyKeyboardMarkup {
	rows := make([][]botgotd.KeyboardButton, 0, (count/3)+4)
	for start := 0; start < count; start += 3 {
		row := make([]botgotd.KeyboardButton, 0, 3)
		for i := start; i < start+3 && i < count; i++ {
			row = append(row, botgotd.KeyboardButton{Text: strconv.Itoa(i + 1)})
		}
		rows = append(rows, row)
	}
	if includeActions {
		rows = append(rows, []botgotd.KeyboardButton{{Text: assets.BtnSemuaTopic}})
		rows = append(rows, []botgotd.KeyboardButton{{Text: assets.BtnHapusTopic}})
	}
	rows = append(rows, []botgotd.KeyboardButton{{Text: assets.BtnBatal}})
	return &botgotd.ReplyKeyboardMarkup{Keyboard: rows, ResizeKeyboard: true}
}

func parseTopicIndices(raw string, maxIndex, maxSelect int) ([]int, error) {
	cleaned := strings.ReplaceAll(raw, ",", " ")
	tokens := strings.Fields(cleaned)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("masukkan nomor topic yang valid")
	}
	seen := make(map[int]bool)
	out := make([]int, 0, len(tokens))
	for _, tok := range tokens {
		if strings.Contains(tok, "-") {
			parts := strings.Split(tok, "-")
			if len(parts) != 2 {
				return nil, fmt.Errorf("format rentang %q tidak valid", tok)
			}
			a, errA := strconv.Atoi(strings.TrimSpace(parts[0]))
			b, errB := strconv.Atoi(strings.TrimSpace(parts[1]))
			if errA != nil || errB != nil || a < 1 || b < 1 || a > maxIndex || b > maxIndex {
				return nil, fmt.Errorf("rentang %q di luar daftar (1–%d)", tok, maxIndex)
			}
			if a > b {
				a, b = b, a
			}
			for n := a; n <= b; n++ {
				if !seen[n] {
					seen[n] = true
					out = append(out, n)
				}
			}
			continue
		}
		n, err := strconv.Atoi(tok)
		if err != nil || n < 1 || n > maxIndex {
			return nil, fmt.Errorf("nomor %q tidak valid (pilih 1–%d)", tok, maxIndex)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if maxSelect > 0 && len(out) > maxSelect {
		return nil, fmt.Errorf("maksimal %d topic per grup (Anda memilih %d)", maxSelect, len(out))
	}
	return out, nil
}

func (h *Handler) CekTopic(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.setPanelState(chatID, "")
	cfg := h.db.GetUserConfig(ctx, userID)
	topics := cfg.Topics
	if len(topics) == 0 {
		if refreshed := h.db.SyncOneFromMongoDB(ctx, userID); refreshed != nil && len(refreshed.Topics) > 0 {
			cfg = refreshed
			topics = refreshed.Topics
		}
	}

	if len(topics) == 0 {
		_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: `<h1>🧑‍💻 Topic Tujuan</h1>
<blockquote expandable>Belum ada grup forum yang diatur topic tujuannya.</blockquote>
<hr/>
<p>Gunakan menu <b>🧑‍💻 Topic 🧑‍💻</b> untuk memilih topic tujuan grup forum Anda.</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.GrupListMenuMarkup,
		})
		return
	}

	groupNameByID := make(map[int64]string, len(cfg.Groups))
	for _, g := range cfg.Groups {
		if g.ChatID != 0 {
			title := strings.TrimSpace(g.Title)
			if title == "" {
				title = g.Target
			}
			groupNameByID[g.ChatID] = title
		}
	}

	order := make([]int64, 0)
	byGroup := make(map[int64][]databases.TopicEntry)
	for _, t := range topics {
		if _, ok := byGroup[t.ChatID]; !ok {
			order = append(order, t.ChatID)
		}
		byGroup[t.ChatID] = append(byGroup[t.ChatID], t)
	}

	var sb strings.Builder
	sb.WriteString("<h1>🧑‍💻 Topic Tujuan</h1>\n")
	sb.WriteString("<p>Grup forum yang dikirim ke topic tertentu:</p>\n")
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString("  <tr><th>#</th><th>🏷️ Grup Forum</th><th>🧵 Topic Tujuan</th></tr>\n")
	for i, gid := range order {
		gTopics := byGroup[gid]
		gName := groupNameByID[gid]
		if gName == "" && len(gTopics) > 0 && gTopics[0].GroupTitle != "" {
			gName = gTopics[0].GroupTitle
		}
		if gName == "" {
			gName = strconv.FormatInt(gid, 10)
		}
		lines := make([]string, 0, len(gTopics))
		for _, t := range gTopics {
			tName := strings.TrimSpace(t.Title)
			if tName == "" {
				tName = fmt.Sprintf("Topic #%d", t.TopicID)
			}
			lines = append(lines, "└ "+html.EscapeString(tName))
		}
		fmt.Fprintf(&sb, "  <tr><td><code>%d</code></td><td><b>%s</b></td><td>%s</td></tr>\n", i+1, html.EscapeString(gName), strings.Join(lines, "<br/>"))
	}
	sb.WriteString("</table>\n")
	fmt.Fprintf(&sb, "<blockquote expandable>ℹ️ Total <b>%d grup forum</b> dengan <b>%d topic tujuan</b> aktif.</blockquote>\n", len(order), len(topics))
	sb.WriteString("<hr/>\n<p>GotdBot • Forum Topic Manager</p>")

	_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: assets.GrupListMenuMarkup,
	})
}

func (h *Handler) renderTopicGroupSelect(ctx context.Context, b *botgotd.Bot, chatID, effectiveID int64, groups []topicForumGroup) {
	var activeCount map[int64]int
	if h.db != nil {
		cfg := h.db.GetUserConfig(ctx, effectiveID)
		activeCount = make(map[int64]int, len(cfg.Topics))
		for _, t := range cfg.Topics {
			activeCount[t.ChatID]++
		}
	} else {
		activeCount = make(map[int64]int)
	}
	var sb strings.Builder
	sb.WriteString("<h1>🧑‍💻 Topic Grup Forum</h1>\n")
	sb.WriteString("<p>Pilih grup yang topic tujuannya ingin diatur:</p>\n")
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString("  <tr><th>#</th><th>🏷️ Grup Forum</th><th>🧵 Topic Aktif</th></tr>\n")
	for i, g := range groups {
		fmt.Fprintf(&sb, "  <tr><td><code>%d</code></td><td><b>%s</b></td><td><code>%d topic</code></td></tr>\n", i+1, html.EscapeString(g.Title), activeCount[g.ChatID])
	}
	sb.WriteString("</table>\n")
	sb.WriteString("<hr/>\n<p>Pilih nomor grup dari keyboard di bawah atau tekan <b>⬅️ Batal ⬅️</b>.</p>")
	msg, _ := b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: topicGroupReplyMarkup(len(groups)),
	})
	if msg != nil && msg.ID > 0 {
		h.setPanelPrompt(chatID, msg.ID)
	}
}

func (h *Handler) StartTopicMenu(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
	h.clearPanelPrompt(ctx, b, chatID)
	h.setPanelState(chatID, "")
	forums, useFastPath, wantsBg, totalGroups := h.cachedForumGroupsFromDB(ctx, chatID, userID)
	if h.db != nil && totalGroups == 0 {
		_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: `<h1>🧑‍💻 Topic Grup Forum</h1>
<blockquote expandable>Tidak ditemukan grup bertipe <b>Forum</b> di daftar grup target Anda. Pastikan Anda sudah menambahkan grup forum melalui <b>📝 Tambah Grup 📝</b>.</blockquote>
<hr/>
<p>GotdBot • Forum Topic Manager</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.GrupListMenuMarkup,
		})
		return
	}
	if useFastPath {
		h.topicMu.Lock()
		h.ensureTopicSessionLocked(chatID).ForumGroups = forums
		h.topicMu.Unlock()
		h.setPanelState(chatID, "topic_select_group")
		h.renderTopicGroupSelect(ctx, b, chatID, userID, forums)
		if wantsBg {
			h.triggerBgForumRefresh(ctx, chatID, userID)
		}
		return
	}
	if h.broker == nil {
		return
	}
	h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: `<h1>🧑‍💻 Topic Grup Forum</h1>
<blockquote expandable>⏳ Memuat daftar grup forum dari akun userbot Anda...</blockquote>`,
		ParseMode: "HTML",
	})
	_ = h.broker.To(ctx, "to:ubot:command", models.PToService{
		ChatID:  chatID,
		UserID:  userID,
		OwnerID: userID,
		Type:    "fetch_forum_groups",
		TaskID:  "select_topic",
	})
}

func (h *Handler) handleTopicServiceResponse(ctx context.Context, b *botgotd.Bot, data models.PFromService) {
	chatID := data.ChatID
	if chatID == 0 {
		return
	}
	effectiveID := h.GetEffectiveUserID(chatID, data.OwnerID)
	if effectiveID == 0 {
		effectiveID = chatID
	}

	switch data.Type {
	case "forum_groups":
		var groups []topicForumGroup
		if err := json.Unmarshal([]byte(data.Text), &groups); err != nil {
			if data.TaskID == "bg_refresh_forum" {
				return
			}
			h.clearProgress(ctx, b, chatID)
			_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        `<h1>❌ Gagal Memuat Grup Forum</h1><blockquote expandable>Respon data grup forum tidak valid.</blockquote><hr/><p>GotdBot • Forum Topic Manager</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: assets.GrupListMenuMarkup,
			})
			return
		}
		if data.TaskID == "bg_refresh_forum" {
			h.topicMu.Lock()
			sess := h.ensureTopicSessionLocked(chatID)
			sess.ForumGroups = groups
			sess.LastForumScan = time.Now()
			h.topicMu.Unlock()
			return
		}
		h.clearProgress(ctx, b, chatID)
		isLink := strings.HasPrefix(data.TaskID, "link_topic")
		if len(groups) == 0 {
			h.setPanelState(chatID, "")
			_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: `<h1>🧑‍💻 Topic Grup Forum</h1>
<blockquote expandable>Tidak ditemukan grup bertipe <b>Forum</b> di daftar grup target Anda. Pastikan Anda sudah menambahkan grup forum melalui <b>📝 Tambah Grup 📝</b>.</blockquote>
<hr/>
<p>GotdBot • Forum Topic Manager</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: assets.GrupListMenuMarkup,
			})
			return
		}

		h.topicMu.Lock()
		sess := h.ensureTopicSessionLocked(chatID)
		sess.ForumGroups = groups
		sess.LastForumScan = time.Now()
		if isLink {
			targetList := ""
			if strings.HasPrefix(data.TaskID, "link_topic:") {
				targetList = strings.TrimPrefix(data.TaskID, "link_topic:")
			}
			sess.LinkTargetList = targetList
			h.topicMu.Unlock()
			h.setPanelState(chatID, "tlist_link_group")
			h.renderLinkGroupSelect(ctx, b, chatID, groups)
			return
		}
		h.topicMu.Unlock()

		h.setPanelState(chatID, "topic_select_group")
		h.renderTopicGroupSelect(ctx, b, chatID, effectiveID, groups)

	case "forum_topics":
		h.clearProgress(ctx, b, chatID)
		var payload topicForumResponse
		if err := json.Unmarshal([]byte(data.Text), &payload); err != nil {
			_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        `<h1>❌ Gagal Memuat Topic</h1><blockquote expandable>Respon daftar topic forum tidak valid.</blockquote><hr/><p>GotdBot • Forum Topic Manager</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: assets.GrupListMenuMarkup,
			})
			return
		}
		if len(payload.Topics) == 0 {
			h.setPanelState(chatID, "")
			_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: fmt.Sprintf(`<h1>🧑‍💻 Pilih Topic</h1>
<blockquote expandable>Tidak ada topic aktif ditemukan di grup <b>%s</b>.</blockquote>
<hr/>
<p>GotdBot • Forum Topic Manager</p>`, html.EscapeString(payload.GroupTitle)),
				ParseMode:   "HTML",
				ReplyMarkup: assets.GrupListMenuMarkup,
			})
			return
		}

		h.topicMu.Lock()
		sess := h.ensureTopicSessionLocked(chatID)
		sess.SelectedGroup = topicForumGroup{ChatID: payload.ChatID, Title: payload.GroupTitle}
		sess.Topics = payload.Topics
		h.topicMu.Unlock()

		if strings.HasPrefix(data.TaskID, "link_topic") {
			h.setPanelState(chatID, "tlist_link_topic")
			h.renderLinkTopicSelect(ctx, b, chatID, effectiveID, payload)
			return
		}

		h.setPanelState(chatID, "topic_select_topics")
		cfg := h.db.GetUserConfig(ctx, effectiveID)
		activeSet := make(map[int]bool)
		for _, t := range cfg.Topics {
			if t.ChatID == payload.ChatID {
				activeSet[t.TopicID] = true
			}
		}

		var sb strings.Builder
		sb.WriteString("<h1>🧑‍💻 Pilih Topic</h1>\n")
		fmt.Fprintf(&sb, "<table border=\"1\">\n  <tr><th>🏷️ Grup</th><td><b>%s</b></td></tr>\n  <tr><th>🧵 Total Topic</th><td><code>%d topic</code></td></tr>\n</table>\n", html.EscapeString(payload.GroupTitle), len(payload.Topics))
		sb.WriteString("<p>Ketik nomor topic tujuan; beberapa sekaligus juga bisa, mis. <code>1 3 5</code> atau rentang <code>2-6</code> (maksimal 10). ✅ = sedang dipakai.</p>\n")
		sb.WriteString("<blockquote expandable>ℹ️ Pesan dikirim ke setiap topic terpilih dengan jeda 1 detik; jeda siaran tetap per grup.</blockquote>\n")
		sb.WriteString("<table border=\"1\">\n")
		sb.WriteString("  <tr><th>#</th><th>🧵 Nama Topic</th><th>Status</th></tr>\n")
		for i, t := range payload.Topics {
			status := "—"
			if activeSet[t.ID] {
				status = "✅ Dipakai"
			}
			fmt.Fprintf(&sb, "  <tr><td><code>%d</code></td><td><b>%s</b></td><td>%s</td></tr>\n", i+1, html.EscapeString(t.Title), status)
		}
		sb.WriteString("</table>\n")
		sb.WriteString("<hr/>\n<p>GotdBot • Forum Topic Manager</p>")

		msg, _ := b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: topicSelectReplyMarkup(len(payload.Topics), true),
		})
		if msg != nil && msg.ID > 0 {
			h.setPanelPrompt(chatID, msg.ID)
		}

	case "tlist_posts", "tlist_post_detail", "tlist_post_saved":
		editID := data.ThreadID
		if editID == 0 {
			editID = h.getTopicListEditID(chatID)
		}
		msgID := h.sendOrEditRich(ctx, b, chatID, editID, data.Text, nil)
		if msgID > 0 {
			h.trackDetailMessageIDs(ctx, chatID, []int{msgID})
		}
	}
}

func (h *Handler) handleTopicPanelInput(ctx context.Context, b *botgotd.Bot, chatID, userID int64, state, text string, message *botgotd.Message) bool {
	switch {
	case state == "topic_select_group":
		h.topicMu.Lock()
		sess := h.ensureTopicSessionLocked(chatID)
		forumGroups := append([]topicForumGroup(nil), sess.ForumGroups...)
		h.topicMu.Unlock()
		idx, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil || idx < 1 || idx > len(forumGroups) {
			_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: fmt.Sprintf(`<h1>⚠️ Pilihan Tidak Valid</h1>
<blockquote expandable>Pilih nomor grup antara <code>1</code> sampai <code>%d</code>, atau tekan <b>⬅️ Batal ⬅️</b>.</blockquote>`, len(forumGroups)),
				ParseMode:   "HTML",
				ReplyMarkup: topicGroupReplyMarkup(len(forumGroups)),
			})
			return true
		}
		h.clearPanelPrompt(ctx, b, chatID)
		selected := forumGroups[idx-1]
		h.topicMu.Lock()
		h.ensureTopicSessionLocked(chatID).SelectedGroup = selected
		h.topicMu.Unlock()
		h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>🧑‍💻 Pilih Topic</h1>
<blockquote expandable>⏳ Mengambil daftar topic dari grup <b>%s</b>...</blockquote>`, html.EscapeString(selected.Title)),
			ParseMode: "HTML",
		})
		if h.broker != nil {
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{
				ChatID:  chatID,
				UserID:  userID,
				OwnerID: userID,
				Type:    "fetch_group_topics",
				TaskID:  "select_topic",
				Text:    strconv.FormatInt(selected.ChatID, 10),
			})
		}
		return true

	case state == "topic_select_topics":
		sess := h.getTopicSession(chatID)
		grp := sess.SelectedGroup
		if text == assets.BtnHapusTopic {
			h.clearPanelPrompt(ctx, b, chatID)
			h.setPanelState(chatID, "")
			_ = h.db.DeleteGroupTopics(ctx, userID, grp.ChatID)
			_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: chatID,
				Text: fmt.Sprintf(`<h1>🗑️ Topic Tujuan Dihapus</h1>
<blockquote expandable>Seluruh pengaturan topic tujuan untuk grup <b>%s</b> telah dihapus.</blockquote>
<hr/>
<p>GotdBot • Forum Topic Manager</p>`, html.EscapeString(grp.Title)),
				ParseMode:   "HTML",
				ReplyMarkup: assets.GrupListMenuMarkup,
			})
			return true
		}

		var chosen []databases.TopicEntry
		if text == assets.BtnSemuaTopic {
			limit := len(sess.Topics)
			if limit > databases.MaxTopicsPerGroup {
				limit = databases.MaxTopicsPerGroup
			}
			for _, t := range sess.Topics[:limit] {
				chosen = append(chosen, databases.TopicEntry{
					ChatID:     grp.ChatID,
					GroupTitle: grp.Title,
					TopicID:    t.ID,
					Title:      t.Title,
				})
			}
		} else {
			indices, err := parseTopicIndices(text, len(sess.Topics), databases.MaxTopicsPerGroup)
			if err != nil {
				_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID: chatID,
					Text: fmt.Sprintf(`<h1>⚠️ Input Topic Tidak Valid</h1>
<blockquote expandable>%s</blockquote>
<hr/>
<p>Contoh: <code>1 3 5</code> atau <code>2-6</code> (maksimal 10).</p>`, html.EscapeString(err.Error())),
					ParseMode:   "HTML",
					ReplyMarkup: topicSelectReplyMarkup(len(sess.Topics), true),
				})
				return true
			}
			sort.Ints(indices)
			for _, idx := range indices {
				t := sess.Topics[idx-1]
				chosen = append(chosen, databases.TopicEntry{
					ChatID:     grp.ChatID,
					GroupTitle: grp.Title,
					TopicID:    t.ID,
					Title:      t.Title,
				})
			}
		}

		h.clearPanelPrompt(ctx, b, chatID)
		h.setPanelState(chatID, "")
		_ = h.db.SetGroupTopics(ctx, userID, grp.ChatID, grp.Title, chosen)

		var sb strings.Builder
		sb.WriteString("<h1>✅ Topic Tujuan Disimpan</h1>\n")
		fmt.Fprintf(&sb, "<table border=\"1\">\n  <tr><th>🏷️ Grup Forum</th><td><b>%s</b></td></tr>\n  <tr><th>🧵 Total Dipilih</th><td><code>%d topic</code></td></tr>\n</table>\n", html.EscapeString(grp.Title), len(chosen))
		sb.WriteString("<blockquote expandable>")
		for i, t := range chosen {
			fmt.Fprintf(&sb, "%d. <b>%s</b>\n", i+1, html.EscapeString(t.Title))
		}
		sb.WriteString("</blockquote>\n<hr/>\n<p>GotdBot • Forum Topic Manager</p>")

		_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        sb.String(),
			ParseMode:   "HTML",
			ReplyMarkup: assets.GrupListMenuMarkup,
		})
		return true

	case state == "tlist_create_name":
		return h.handleTopicListCreateInput(ctx, b, chatID, userID, text)

	case strings.HasPrefix(state, "tlist_add_post:"):
		listName := strings.TrimPrefix(state, "tlist_add_post:")
		return h.handleTopicListAddPostText(ctx, b, chatID, userID, listName, text, message)

	case state == "tlist_link_group":
		return h.handleTopicListLinkGroupInput(ctx, b, chatID, userID, text)

	case state == "tlist_link_topic":
		return h.handleTopicListLinkTopicInput(ctx, b, chatID, userID, text)
	}
	return false
}
