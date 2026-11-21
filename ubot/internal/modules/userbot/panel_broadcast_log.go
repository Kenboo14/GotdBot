package userbot

import (
	"assterisk/ubot/internal/models"
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type panelRoundFailure struct {
	Title  string
	Reason string
}

func isPanelNotifikasiOff(notif string) bool {
	lower := strings.ToLower(strings.TrimSpace(notif))
	return strings.Contains(lower, "nonaktif") || strings.Contains(lower, "mati")
}

func panelGroupLabel(grp jasebGroupDoc) string {
	if title := strings.TrimSpace(grp.Title); title != "" {
		return title
	}
	if tgt := strings.TrimSpace(grp.Target); tgt != "" {
		return tgt
	}
	return fmt.Sprintf("%d", grp.ChatID)
}

func extractTrailingSeconds(upperErr, prefix string) int {
	idx := strings.Index(upperErr, prefix)
	if idx < 0 {
		return 0
	}
	rest := upperErr[idx+len(prefix):]
	digits := make([]byte, 0, 8)
	for i := 0; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			break
		}
		digits = append(digits, rest[i])
	}
	if len(digits) == 0 {
		return 0
	}
	v, err := strconv.Atoi(string(digits))
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

func humanizePanelBroadcastError(err error) string {
	if err == nil {
		return ""
	}
	if flood, ok := tgerr.AsFloodWait(err); ok {
		sec := int(math.Ceil(flood.Seconds()))
		if sec <= 0 {
			sec = 1
		}
		return fmt.Sprintf("flood wait, tunggu %d detik", sec)
	}

	raw := strings.TrimSpace(err.Error())
	upper := strings.ToUpper(raw)

	if sec := extractTrailingSeconds(upper, "SLOWMODE_WAIT_"); sec > 0 {
		return fmt.Sprintf("slowmode aktif, tunggu %d detik", sec)
	}
	if sec := extractTrailingSeconds(upper, "FLOOD_WAIT_"); sec > 0 {
		return fmt.Sprintf("flood wait, tunggu %d detik", sec)
	}

	switch {
	case strings.Contains(upper, "SLOWMODE_WAIT"):
		return "slowmode aktif"
	case strings.Contains(upper, "CHAT_SEND_PLAIN_FORBIDDEN"):
		return "hanya media yang diperbolehkan"
	case strings.Contains(upper, "CHAT_SEND_MEDIA_FORBIDDEN"),
		strings.Contains(upper, "CHAT_SEND_PHOTOS_FORBIDDEN"),
		strings.Contains(upper, "CHAT_SEND_VIDEOS_FORBIDDEN"),
		strings.Contains(upper, "CHAT_SEND_DOCS_FORBIDDEN"),
		strings.Contains(upper, "CHAT_SEND_GIFS_FORBIDDEN"),
		strings.Contains(upper, "CHAT_SEND_STICKERS_FORBIDDEN"):
		return "pengiriman media tidak diperbolehkan"
	case strings.Contains(upper, "CHAT_WRITE_FORBIDDEN"):
		return "tidak memiliki izin kirim pesan"
	case strings.Contains(upper, "CHAT_ADMIN_REQUIRED"):
		return "hanya admin yang dapat mengirim pesan"
	case strings.Contains(upper, "USER_BANNED_IN_CHANNEL"):
		return "akun dibatasi di grup ini"
	case strings.Contains(upper, "CHANNEL_PRIVATE"),
		strings.Contains(upper, "CHANNEL_INVALID"),
		strings.Contains(upper, "CHAT_ID_INVALID"):
		return "grup privat atau tidak tersedia"
	case strings.Contains(upper, "CHAT_RESTRICTED"):
		return "grup dibatasi oleh Telegram"
	case strings.Contains(upper, "TOPIC_CLOSED"),
		strings.Contains(upper, "TOPIC_DELETED"):
		return "topik forum ditutup atau dihapus"
	case strings.Contains(upper, "ALLOW_PAYMENT_REQUIRED"):
		return "grup membutuhkan pembayaran Stars"
	case strings.Contains(upper, "PEER_FLOOD"):
		return "terkena limit spam Telegram (peer flood)"
	case strings.Contains(upper, "CHAT_FORBIDDEN"):
		return "tidak memiliki akses ke grup ini"
	case strings.Contains(upper, "MSG_ID_INVALID"):
		return "materi cache tidak ditemukan"
	case strings.Contains(upper, "ACCESS_HASH"):
		return "grup belum sinkron atau userbot belum bergabung"
	case errors.Is(err, context.DeadlineExceeded):
		return "waktu koneksi Telegram habis"
	}

	clean := raw
	if idx := strings.LastIndex(clean, ":"); idx >= 0 && idx+1 < len(clean) {
		candidate := strings.TrimSpace(clean[idx+1:])
		if candidate != "" {
			clean = candidate
		}
	}
	if len([]rune(clean)) > 90 {
		clean = string([]rune(clean)[:90]) + "…"
	}
	return strings.ToLower(clean)
}

func truncateSnippetRunes(text string, limit int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || limit <= 0 {
		return trimmed
	}
	runes := []rune(trimmed)
	if len(runes) <= limit {
		return trimmed
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

func (s *Service) resolvePanelPostMeta(ctx context.Context, client *gobot.Client, cachePeer tg.InputPeerClass, cfg jasebConfigDoc, msgID int) (string, string) {
	delayLabel := "DELAY PERGRUP"
	modeLower := strings.ToLower(strings.TrimSpace(cfg.DelayMode))
	if modeLower != "aman" && modeLower != "otomatis" && cfg.Delay == 0 && cfg.DelayAllGroups > 0 {
		delayLabel = "DELAY ALLGROUP"
	}

	modeName := strings.TrimSpace(cfg.Mode)
	if modeName == "" {
		modeName = "Normal"
	}

	mediaKind := "Text"
	if s != nil && msgID > 0 {
		if album := s.getAlbumMapping(ctx, msgID); len(album) > 1 {
			mediaKind = "Album"
		}
	}

	var cachedMsg *tg.Message
	if msgID > 0 {
		cachedMsg = getCachedPanelMsg(msgID)
		if cachedMsg == nil && s != nil && client != nil && cachePeer != nil {
			fetchCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			if fetched, err := s.fetchCacheMessage(fetchCtx, client, cachePeer, msgID); err == nil {
				cachedMsg = fetched
			}
			cancel()
		}
	}

	if cachedMsg != nil && mediaKind != "Album" {
		if gid, ok := cachedMsg.GetGroupedID(); ok && gid != 0 {
			mediaKind = "Album"
		} else if cachedMsg.Media != nil {
			mediaKind = "Media"
		}
	}

	snippet := ""
	if cachedMsg != nil && strings.TrimSpace(cachedMsg.Message) != "" {
		snippet = truncateSnippetRunes(cachedMsg.Message, 160)
	} else if strings.TrimSpace(cfg.Watermark) != "" {
		snippet = truncateSnippetRunes(cfg.Watermark, 160)
	} else if msgID > 0 {
		snippet = fmt.Sprintf("Materi List #%d (%s)", msgID, mediaKind)
	} else {
		snippet = fmt.Sprintf("Materi Promosi (%s)", mediaKind)
	}

	headerTag := fmt.Sprintf("%s [%s %s]", delayLabel, modeName, mediaKind)
	return snippet, headerTag
}

func formatPanelRoundLogHTML(snippet, headerTag string, putaran, sukses int, failures []panelRoundFailure) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<blockquote>%s</blockquote>\n\n", html.EscapeString(snippet))
	fmt.Fprintf(&b, "<b>%s</b>\n", html.EscapeString(headerTag))
	fmt.Fprintf(&b, "<b>Putaran :</b> <i>%d</i>", putaran)
	if sukses > 0 {
		fmt.Fprintf(&b, "\n<b>Sukses :</b> <i>%d</i>", sukses)
	}
	if len(failures) == 1 && sukses == 0 {
		fmt.Fprintf(&b, "\n<b>Gagal :</b> <i>%s</i>", html.EscapeString(failures[0].Title))
		fmt.Fprintf(&b, "\n<b>Alasan :</b> <i>%s</i>", html.EscapeString(failures[0].Reason))
	} else if len(failures) > 0 {
		fmt.Fprintf(&b, "\n<b>Gagal :</b> <i>%d</i>", len(failures))
		b.WriteString("\n<b>Alasan :</b>")
		maxItems := 15
		for i, f := range failures {
			if i >= maxItems {
				fmt.Fprintf(&b, "\n<i>... dan %d grup lainnya</i>", len(failures)-i)
				break
			}
			fmt.Fprintf(&b, "\n%d. <i>%s : %s</i>", i+1, html.EscapeString(f.Title), html.EscapeString(f.Reason))
		}
	}
	return b.String()
}

func formatPanelFloodLogHTML(snippet, headerTag, groupTitle string, floodDur time.Duration, skipped, isPeerFlood bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<blockquote>%s</blockquote>\n\n", html.EscapeString(snippet))
	if isPeerFlood {
		fmt.Fprintf(&b, "⚠️ <b>FLOOD TERDETEKSI (%s)</b>\n", html.EscapeString(headerTag))
		fmt.Fprintf(&b, "<b>Grup :</b> <i>%s</i>\n", html.EscapeString(groupTitle))
		b.WriteString("<b>Alasan :</b> <i>terkena limit spam Telegram (PEER_FLOOD)</i>\n")
		b.WriteString("<b>Status :</b> <i>Lanjut ke grup berikutnya (siaran tetap berjalan)</i>")
		return b.String()
	}

	sec := int(math.Ceil(floodDur.Seconds()))
	if sec <= 0 {
		sec = 1
	}
	fmt.Fprintf(&b, "⏳ <b>FLOOD WAIT TERDETEKSI (%s)</b>\n", html.EscapeString(headerTag))
	fmt.Fprintf(&b, "<b>Grup :</b> <i>%s</i>\n", html.EscapeString(groupTitle))
	fmt.Fprintf(&b, "<b>Durasi Flood :</b> <i>%d detik</i>\n", sec)
	if skipped {
		b.WriteString("<b>Tindakan :</b> <i>Melewati grup (melebihi batas Skip FloodWait)</i>")
	} else {
		b.WriteString("<b>Tindakan :</b> <i>Menunggu flood selesai lalu mengirim ulang</i>")
	}
	return b.String()
}

func (s *Service) emitPanelBroadcastLog(ctx context.Context, cfg jasebConfigDoc, htmlText string) {
	if s == nil || s.redis == nil || cfg.OwnerID == 0 || strings.TrimSpace(htmlText) == "" {
		return
	}
	if isPanelNotifikasiOff(cfg.Notifikasi) {
		return
	}
	sendCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.redis.To(sendCtx, "from:ubot", &models.PFromService{
		ChatID:  cfg.OwnerID,
		OwnerID: cfg.OwnerID,
		Type:    "panel_broadcast_log",
		Text:    htmlText,
	})
}

func (s *Service) emitPanelFloodNotification(ctx context.Context, client *gobot.Client, cachePeer tg.InputPeerClass, cfg jasebConfigDoc, grp jasebGroupDoc, msgID int, floodDur time.Duration, skipped, isPeerFlood bool) {
	if s == nil || isPanelNotifikasiOff(cfg.Notifikasi) {
		return
	}
	snippet, headerTag := s.resolvePanelPostMeta(ctx, client, cachePeer, cfg, msgID)
	msg := formatPanelFloodLogHTML(snippet, headerTag, panelGroupLabel(grp), floodDur, skipped, isPeerFlood)
	s.emitPanelBroadcastLog(ctx, cfg, msg)
}
