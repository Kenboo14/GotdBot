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
	"log"
	"strconv"
	"strings"
	"time"
)

func ParseManualGroupTokens(raw string) []string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
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

func (h *Handler) handleManualGroupPanelInput(
	ctx context.Context,
	b *botgotd.Bot,
	chatID int64,
	targetID int64,
	text string,
	message *botgotd.Message,
	isRemote bool,
	parentMsgID int,
) {
	if isRemote && (text == assets.BtnBatal || strings.EqualFold(text, "batal") || text == "/cancel") {
		if parentMsgID > 0 {
			h.RenderControlGrupList(ctx, b, chatID, parentMsgID, targetID)
		}
		if message != nil {
			_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: []int{message.ID}})
		}
		return
	}

	if policy, enforced := h.getEffectiveTierPolicy(ctx, targetID); enforced && policy.MaxGroups > 0 {
		gCount, _ := h.db.CountGroupsAndLists(ctx, targetID)
		if gCount >= policy.MaxGroups {
			markup := assets.GrupListMenuMarkup
			if isRemote {
				markup = nil
			}
			h.denyTierFeature(ctx, b, chatID, policy.Tier, fmt.Sprintf("Tambah Grup (> %d grup)", policy.MaxGroups), "Pro", markup)
			return
		}
	}

	tokens := ParseManualGroupTokens(text)
	if len(tokens) == 0 {
		if !isRemote {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        "⚠️ Tidak ada target grup valid yang ditemukan. Coba kirim lagi.",
				ReplyMarkup: assets.GrupListMenuMarkup,
			})
		}
		return
	}

	if isRemote && message != nil {
		_, _ = b.DeleteMessages(ctx, &botgotd.DeleteMessagesParams{ChatID: chatID, MessageIDs: []int{message.ID}})
	}

	if h.broker != nil {
		brokerCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		payload := models.PToService{
			ChatID:  chatID,
			UserID:  targetID,
			OwnerID: targetID,
			Type:    "add_groups_manual",
			Text:    strings.Join(tokens, "\n"),
		}
		if err := h.broker.To(brokerCtx, "to:ubot:command", payload); err == nil {
			if isRemote {
				targetName := h.GetAccountName(targetID)
				if targetName == "" {
					targetName = fmt.Sprintf("Akun [%d]", targetID)
				}
				if parentMsgID > 0 {
					_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
						ChatID:      chatID,
						MessageID:   parentMsgID,
						Text:        fmt.Sprintf("<h1>⏳ Memproses Grup Manual</h1><hr/><b>👑 Akun Target:</b> %s (<code>%d</code>)<blockquote expandable>Worker userbot sedang memvalidasi &amp; melakukan Auto-Join untuk <b>%d</b> target grup. Hasil akan dikirim otomatis.</blockquote>", html.EscapeString(targetName), targetID, len(tokens)),
						ParseMode:   "HTML",
						ReplyMarkup: assets.CtrlDoneIKB(targetID),
					})
				}
			} else {
				h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
					ChatID:      chatID,
					Text:        fmt.Sprintf("<h1>⏳ Memproses %d Target Grup</h1><blockquote expandable>Worker userbot sedang memvalidasi ID/username dan melakukan Auto-Join untuk link privat/publik. Hasil akan dikirim otomatis.</blockquote><hr/><p>GotdBot • Manual Group Manager</p>", len(tokens)),
					ParseMode:   "HTML",
					ReplyMarkup: assets.GrupListMenuMarkup,
				})
			}
			return
		} else {
			log.Printf("add_groups_manual broker error: %v", err)
		}
	}

	added := 0
	for _, tok := range tokens {
		chatIDVal, _ := strconv.ParseInt(tok, 10, 64)
		if err := h.db.AddGroup(ctx, targetID, tok, tok, chatIDVal); err == nil {
			added++
		}
	}
	if isRemote {
		targetName := h.GetAccountName(targetID)
		if targetName == "" {
			targetName = fmt.Sprintf("Akun [%d]", targetID)
		}
		if parentMsgID > 0 {
			_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:      chatID,
				MessageID:   parentMsgID,
				Text:        assets.TxtControlGrupSentSuccess(targetName, targetID, added),
				ParseMode:   "HTML",
				ReplyMarkup: assets.CtrlDoneIKB(targetID),
			})
		}
		return
	}
	if added > 0 {
		h.resetTopicForumScan(chatID)
	}
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        fmt.Sprintf("<h1>✅ Grup Manual Ditambahkan</h1><hr/>Berhasil menyimpan <b>%d</b> target grup.<hr/><p>GotdBot • Manual Group Manager</p>", added),
		ParseMode:   "HTML",
		ReplyMarkup: assets.GrupListMenuMarkup,
	})
}

func (h *Handler) applySyncedManualGroups(ctx context.Context, ownerID int64, rawJSON string) {
	if h == nil || h.db == nil || ownerID == 0 || strings.TrimSpace(rawJSON) == "" {
		return
	}
	var incoming []databases.GroupEntry
	if err := json.Unmarshal([]byte(rawJSON), &incoming); err != nil || len(incoming) == 0 {
		return
	}
	cfg := h.db.GetUserConfig(ctx, ownerID)
	changed := false
	for _, item := range incoming {
		matchedIdx := -1
		for i, existing := range cfg.Groups {
			if (existing.Target != "" && existing.Target == item.Target) || databases.SameGroupID(existing.ChatID, item.ChatID) {
				matchedIdx = i
				break
			}
		}
		if matchedIdx >= 0 {
			cur := &cfg.Groups[matchedIdx]
			if item.Title != "" && cur.Title != item.Title {
				cur.Title = item.Title
				changed = true
			}
			if item.Target != "" && !strings.HasPrefix(cur.Target, "@") && cur.Target != item.Target {
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
			continue
		}
		cfg.Groups = append(cfg.Groups, item)
		changed = true
	}
	if changed {
		if err := h.db.SaveUserConfig(ctx, cfg); err != nil {
			log.Printf("applySyncedManualGroups save error (owner=%d): %v", ownerID, err)
		}
	}
}
