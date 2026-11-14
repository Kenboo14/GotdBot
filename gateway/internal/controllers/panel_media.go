package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// HandlePanelMediaUpdate handles incoming media updates for cache capture and wizard inputs
func (h *Handler) HandlePanelMediaUpdate(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if h.captureCacheMedia(update) {
		return
	}
	h.HandlePanelMedia(ctx, b, update)
}

func (h *Handler) captureCacheMedia(update *botgotd.Update) bool {
	if update == nil {
		return false
	}
	message := update.Message
	if message == nil {
		message = update.ChannelPost
	}
	if message == nil {
		return false
	}
	cache := strings.TrimPrefix(strings.TrimSpace(os.Getenv("GROUP_CACHE")), "@")
	if cache == "" {
		cache = "AiotdCaches"
	}
	chatID := strconv.FormatInt(message.Chat.ID, 10)
	chatMatches := strings.EqualFold(strings.TrimSpace(message.Chat.Username), cache) || chatID == cache || "-100"+chatID == cache
	if !chatMatches {
		return false
	}
	item := cachedMediaFile{}
	if len(message.Photo) > 0 {
		item.Kind, item.FileID = "photo", message.Photo[len(message.Photo)-1].FileID
	} else if message.Video != nil {
		item.Kind, item.FileID = "video", message.Video.FileID
	} else if message.Document != nil {
		item.Kind, item.FileID = "document", message.Document.FileID
	} else if message.Audio != nil {
		item.Kind, item.FileID = "audio", message.Audio.FileID
	} else if message.Animation != nil {
		item.Kind, item.FileID = "animation", message.Animation.FileID
	} else if message.Voice != nil {
		item.Kind, item.FileID = "voice_note", message.Voice.FileID
	} else {
		return true
	}
	h.cacheMediaMu.Lock()
	h.cacheMedia[message.ID] = item
	h.cacheMediaMu.Unlock()
	return true
}

// HandlePanelMedia routes media/caption messages into the same panel state flow.
func (h *Handler) HandlePanelMedia(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return
	}
	message := update.Message
	state := h.getPanelState(message.Chat.ID)
	if state != "waiting_add_list" && state != "waiting_add_watermark" && !strings.HasPrefix(state, "ctrl_add_list:") {
		return
	}
	if message.MediaGroupID != "" {
		effectiveUID := message.From.ID
		if strings.HasPrefix(state, "ctrl_add_list:") {
			parts := strings.Split(state, ":")
			if len(parts) >= 2 {
				if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					effectiveUID = id
				}
			}
		}
		key := fmt.Sprintf("%d:%s:%s", message.Chat.ID, state, message.MediaGroupID)
		h.mediaMu.Lock()
		batch := h.mediaBatches[key]
		if batch == nil {
			batch = &panelMediaBatch{ChatID: message.Chat.ID, UserID: effectiveUID, State: state}
			h.mediaBatches[key] = batch
		}
		batch.SourceIDs = append(batch.SourceIDs, message.ID)
		// Ambil caption dari foto/video pertama yang membawa teks
		if batch.Caption == "" {
			if message.Caption != "" {
				batch.Caption = message.Caption
				batch.Entities = panelEntities(message)
			} else if message.Text != "" {
				batch.Caption = message.Text
				batch.Entities = panelEntities(message)
			}
		}
		if batch.Timer != nil {
			batch.Timer.Stop()
		}
		batch.Timer = time.AfterFunc(1500*time.Millisecond, func() { h.flushPanelMediaGroup(context.Background(), b, key) })
		h.mediaMu.Unlock()
		return
	}
	caption := message.Caption
	if caption == "" {
		caption = message.Text
	}
	effectiveUID := message.From.ID
	if strings.HasPrefix(state, "ctrl_add_list:") {
		parts := strings.Split(state, ":")
		if len(parts) >= 2 {
			if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				effectiveUID = id
			}
		}
	}
	h.stagePanelMediaWithCaption(ctx, b, message.Chat.ID, effectiveUID, state, []int{message.ID}, caption, panelEntities(message))
}

func (h *Handler) flushPanelMediaGroup(ctx context.Context, b *botgotd.Bot, key string) {
	h.mediaMu.Lock()
	batch := h.mediaBatches[key]
	delete(h.mediaBatches, key)
	h.mediaMu.Unlock()
	if batch == nil {
		return
	}
	h.stagePanelMediaWithCaption(ctx, b, batch.ChatID, batch.UserID, batch.State, batch.SourceIDs, batch.Caption, batch.Entities)
}

func (h *Handler) stagePanelMedia(ctx context.Context, b *botgotd.Bot, chatID, userID int64, state string, sourceIDs []int) {
	h.stagePanelMediaWithCaption(ctx, b, chatID, userID, state, sourceIDs, "", nil)
}

func (h *Handler) stagePanelMediaWithCaption(ctx context.Context, b *botgotd.Bot, chatID, userID int64, state string, sourceIDs []int, caption string, entities []models.MessageEntity) {
	h.clearPanelPrompt(ctx, b, chatID)
	if strings.HasPrefix(state, "ctrl_add_list:") {
		parts := strings.Split(state, ":")
		parentMsgID := 0
		targetID := userID
		if len(parts) >= 2 {
			if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				targetID = id
			}
		}
		if len(parts) >= 3 {
			parentMsgID, _ = strconv.Atoi(parts[2])
		}
		h.setPanelState(chatID, "")
		brokerCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		payload := models.PToService{
			ChatID:           0, // 0 agar ubot tidak membalas via SendMessage + ReplyKeyboard
			UserID:           targetID,
			OwnerID:          targetID,
			Type:             "cache_list",
			MediaSourceCount: len(sourceIDs),
			Text:             caption,
			Entities:         entities,
		}
		_ = h.broker.To(brokerCtx, "to:ubot:command", payload)

		if parentMsgID > 0 {
			targetName := h.GetAccountName(targetID)
			if targetName == "" {
				targetName = fmt.Sprintf("Akun [%d]", targetID)
			}
			_, _ = b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
				ChatID:      chatID,
				MessageID:   parentMsgID,
				Text:        assets.TxtControlMediaSaving(targetName, targetID),
				ParseMode:   "HTML",
				ReplyMarkup: assets.CtrlDoneIKB(targetID),
			})
		}
		return
	}

	jobType := "cache_list"
	if state == "waiting_add_watermark" {
		jobType = "cache_watermark"
	}
	h.setPanelState(chatID, "")
	payload := models.PToService{
		ChatID:           chatID,
		UserID:           userID,
		Type:             jobType,
		MediaSourceCount: len(sourceIDs),
		Text:             caption,
		Entities:         entities,
	}
	if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
		log.Printf("media cache broker error: %v", err)
		return
	}
	h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        "⏳ <b>Mengamankan media ke cache...</b>",
		ParseMode:   "HTML",
		ReplyMarkup: grupListMenuMarkup,
	})
}

func panelEntities(message *botgotd.Message) []models.MessageEntity {
	if message == nil {
		return nil
	}
	entities := message.Entities
	if message.Caption != "" {
		entities = message.CaptionEntities
	}
	result := make([]models.MessageEntity, 0, len(entities))
	for _, entity := range entities {
		item := models.MessageEntity{Type: string(entity.Type), Offset: entity.Offset, Length: entity.Length, URL: entity.URL, Language: entity.Language, CustomEmojiID: entity.CustomEmojiID}
		if entity.User != nil {
			item.UserID = entity.User.ID
		}
		result = append(result, item)
	}
	return result
}
