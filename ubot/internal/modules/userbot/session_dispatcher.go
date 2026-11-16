package userbot

import (
	"context"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

type userbotUpdateHandler struct {
	dispatcher tg.UpdateDispatcher
	s          *Service
	chatID     int64
	clientRef  **telegram.Client
}

func (h *userbotUpdateHandler) Handle(ctx context.Context, u tg.UpdatesClass) error {
	h.s.recordIncomingUpdate(h.chatID)

	switch upd := u.(type) {
	case *tg.UpdateShort:
		if _, ok := upd.Update.(*tg.UpdateServiceNotification); ok {
			h.s.triggerImmediateVerification(h.chatID)
		}
		return h.dispatcher.Handle(ctx, u)

	case *tg.UpdateShortMessage:
		if upd.UserID == 777000 {
			h.s.triggerImmediateVerification(h.chatID)
		}
		h.s.mu.RLock()
		ownerID := h.s.owners[h.chatID]
		h.s.mu.RUnlock()

		var fromPeer tg.PeerClass
		if upd.Out {
			fromPeer = &tg.PeerUser{UserID: ownerID}
		} else {
			fromPeer = &tg.PeerUser{UserID: upd.UserID}
		}

		msg := &tg.Message{
			Flags:       upd.Flags,
			Out:         upd.Out,
			Mentioned:   upd.Mentioned,
			MediaUnread: upd.MediaUnread,
			Silent:      upd.Silent,
			ID:          upd.ID,
			FromID:      fromPeer,
			PeerID:      &tg.PeerUser{UserID: upd.UserID},
			Message:     upd.Message,
			Date:        upd.Date,
			FwdFrom:     upd.FwdFrom,
			ViaBotID:    upd.ViaBotID,
			ReplyTo:     upd.ReplyTo,
			Entities:    upd.Entities,
			TTLPeriod:   upd.TTLPeriod,
		}
		if upd.ReplyTo != nil {
			msg.SetReplyTo(upd.ReplyTo)
		}

		return h.dispatcher.Handle(ctx, &tg.Updates{
			Updates: []tg.UpdateClass{
				&tg.UpdateNewMessage{
					Message:  msg,
					Pts:      upd.Pts,
					PtsCount: upd.PtsCount,
				},
			},
		})

	case *tg.UpdateShortChatMessage:
		h.s.mu.RLock()
		ownerID := h.s.owners[h.chatID]
		h.s.mu.RUnlock()

		var fromPeer tg.PeerClass
		if upd.Out {
			fromPeer = &tg.PeerUser{UserID: ownerID}
		} else {
			fromPeer = &tg.PeerUser{UserID: upd.FromID}
		}

		msg := &tg.Message{
			Flags:       upd.Flags,
			Out:         upd.Out,
			Mentioned:   upd.Mentioned,
			MediaUnread: upd.MediaUnread,
			Silent:      upd.Silent,
			ID:          upd.ID,
			FromID:      fromPeer,
			PeerID:      &tg.PeerChat{ChatID: upd.ChatID},
			Message:     upd.Message,
			Date:        upd.Date,
			FwdFrom:     upd.FwdFrom,
			ViaBotID:    upd.ViaBotID,
			ReplyTo:     upd.ReplyTo,
			Entities:    upd.Entities,
			TTLPeriod:   upd.TTLPeriod,
		}
		if upd.ReplyTo != nil {
			msg.SetReplyTo(upd.ReplyTo)
		}

		return h.dispatcher.Handle(ctx, &tg.Updates{
			Updates: []tg.UpdateClass{
				&tg.UpdateNewMessage{
					Message:  msg,
					Pts:      upd.Pts,
					PtsCount: upd.PtsCount,
				},
			},
		})

	case *tg.Updates:
		for _, item := range upd.Updates {
			if _, ok := item.(*tg.UpdateServiceNotification); ok {
				h.s.triggerImmediateVerification(h.chatID)
				break
			}
		}
		return h.dispatcher.Handle(ctx, u)

	case *tg.UpdatesCombined:
		for _, item := range upd.Updates {
			if _, ok := item.(*tg.UpdateServiceNotification); ok {
				h.s.triggerImmediateVerification(h.chatID)
				break
			}
		}
		return h.dispatcher.Handle(ctx, u)

	default:
		return h.dispatcher.Handle(ctx, u)
	}
}
