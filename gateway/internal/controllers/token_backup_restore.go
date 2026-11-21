package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
)

func applyPortableBackupToConfig(
	cfg *databases.JasebConfig,
	p *PortableBackupPayload,
	policy databases.TierPolicy,
	enforced bool,
) (int, int, int, int, []string) {
	lists := append([]int(nil), p.Msg...)
	groups := make([]databases.GroupEntry, 0, len(p.GC)+len(p.NamedTargets))
	seenGroup := make(map[string]bool)

	for _, id := range p.GC {
		if id == 0 {
			continue
		}
		key := strconv.FormatInt(id, 10)
		if seenGroup[key] {
			continue
		}
		seenGroup[key] = true
		peerType := "chat"
		if strings.HasPrefix(key, "-100") {
			peerType = "channel"
		}
		meta := p.GCExtra[key]
		if meta.PeerType != "" {
			peerType = meta.PeerType
		}
		target := key
		if meta.Target != "" {
			target = meta.Target
		}
		title := strings.TrimSpace(p.GCUser[key])
		if title == "" {
			title = target
		}
		groups = append(groups, databases.GroupEntry{
			Target:     target,
			Title:      title,
			ChatID:     id,
			AccessHash: meta.AccessHash,
			PeerType:   peerType,
			IsForum:    meta.IsForum,
		})
	}
	for _, nt := range p.NamedTargets {
		if strings.TrimSpace(nt.Target) == "" || seenGroup[nt.Target] {
			continue
		}
		seenGroup[nt.Target] = true
		title := strings.TrimSpace(p.GCUser[nt.Target])
		if title == "" {
			title = nt.Target
		}
		peerType := nt.PeerType
		if peerType == "" {
			peerType = "channel"
		}
		groups = append(groups, databases.GroupEntry{
			Target:     nt.Target,
			Title:      title,
			AccessHash: nt.AccessHash,
			PeerType:   peerType,
			IsForum:    nt.IsForum,
		})
	}

	var topics []databases.TopicEntry
	for _, items := range p.Topic {
		for _, item := range items {
			if item.TopicID == 0 {
				continue
			}
			topics = append(topics, databases.TopicEntry{
				ChatID:     item.ChatID,
				GroupTitle: item.GroupTitle,
				TopicID:    item.TopicID,
				Title:      item.Title,
			})
		}
	}
	topicLists := make([]databases.TopicListEntry, 0, len(p.TList))
	for mapName, tl := range p.TList {
		name := strings.TrimSpace(tl.Name)
		if name == "" {
			name = mapName
		}
		if name == "" {
			continue
		}
		playMode := strings.TrimSpace(tl.PlayMode)
		if playMode == "" {
			playMode = "ordered"
		}
		topicLists = append(topicLists, databases.TopicListEntry{
			Name:         name,
			Posts:        append([]int(nil), tl.Posts...),
			PlayMode:     playMode,
			LinkedTopics: append([]databases.TopicLink(nil), tl.LinkedTopics...),
		})
	}

	var notes []string
	if enforced && policy.MaxLists > 0 && len(lists) > policy.MaxLists {
		lists = lists[:policy.MaxLists]
		notes = append(notes, fmt.Sprintf("List dibatasi %d item (paket %s)", policy.MaxLists, policy.Tier))
	}
	if enforced && policy.MaxGroups > 0 && len(groups) > policy.MaxGroups {
		groups = groups[:policy.MaxGroups]
		notes = append(notes, fmt.Sprintf("Grup dibatasi %d target (paket %s)", policy.MaxGroups, policy.Tier))
	}
	if enforced && !policy.AllowTopic && (len(topics) > 0 || len(topicLists) > 0) {
		topics = []databases.TopicEntry{}
		topicLists = []databases.TopicListEntry{}
		notes = append(notes, fmt.Sprintf("Topic tidak dipulihkan pada paket %s", policy.Tier))
	}

	cfg.Lists = lists
	cfg.Groups = groups
	cfg.Topics = topics
	cfg.TopicLists = topicLists
	cfg.TopicEnabled = len(topics) > 0

	jeda := p.JedaGrup
	if jeda == 0 {
		jeda = p.Jeda
	}
	cfg.Delay = jeda
	cfg.DelayAllGroups = p.JedaPutar
	if dm := strings.TrimSpace(p.DelayMode); dm != "" {
		cfg.DelayMode = dm
	}
	cfg.Safemode = p.SafeMode
	if p.FloodSkip > 0 {
		cfg.SkipFloodWait = true
		cfg.SkipFloodWaitSec = p.FloodSkip
	} else {
		cfg.SkipFloodWait = p.FloodSkipOn
		cfg.SkipFloodWaitSec = 0
	}
	if m := parseBackupStringField(p.Mode); m != "" {
		if strings.EqualFold(m, "pro") || strings.EqualFold(m, "forward") {
			cfg.Mode = "Forward"
		} else if strings.EqualFold(m, "biasa") || strings.EqualFold(m, "normal") {
			cfg.Mode = "Normal"
		} else {
			cfg.Mode = m
		}
	}
	if t := parseBackupStringField(p.Timer); t != "" {
		cfg.Timer = t
	}
	if a := parseBackupStringField(p.Admin); a != "" {
		cfg.Admin = a
	}
	if strings.TrimSpace(p.Email) != "" {
		cfg.Email = strings.TrimSpace(p.Email)
	}
	if tn := parseBackupStringField(p.TypeNotif); tn != "" {
		cfg.Notifikasi = tn
	} else if !p.Notif {
		cfg.Notifikasi = "Nonaktif"
	} else {
		cfg.Notifikasi = "Aktif · Kirim ke Saya"
	}
	cfg.Watermark = p.WM
	cfg.WatermarkCacheID = p.WMCacheID

	if enforced && !policy.AllowCustomWatermark && (cfg.Watermark != "" || cfg.WatermarkCacheID != 0) {
		cfg.Watermark = ""
		cfg.WatermarkCacheID = 0
		notes = append(notes, fmt.Sprintf("Custom Watermark dinonaktifkan pada paket %s", policy.Tier))
	}
	if enforced && !policy.AllowManualJeda {
		cfg.DelayMode = "aman"
		cfg.Delay = 0
		cfg.DelayAllGroups = 0
		cfg.SkipFloodWait = false
		cfg.SkipFloodWaitSec = 0
	}
	if enforced && !policy.AllowForwardMode && strings.EqualFold(cfg.Mode, "Forward") {
		cfg.Mode = "Normal"
	}

	return len(lists), len(groups), len(topics), len(topicLists), notes
}

func (h *Handler) createAndRenderDataBackup(ctx context.Context, b *botgotd.Bot, chatID, userID int64, editMsgID int) {
	cfg := h.db.GetUserConfig(ctx, userID)
	lic, _ := h.db.GetActiveUserLicense(ctx, userID)
	code, err := EncodePortableBackup(cfg, lic)
	if err != nil {
		errHTML := `<h1>❌ Gagal Membuat Backup</h1><blockquote expandable>Terjadi kesalahan saat menyusun kode backup data Anda.</blockquote><hr/><p>GotdBot • Data Backup Manager</p>`
		h.sendOrEditBackupView(ctx, b, chatID, editMsgID, errHTML, assets.SettingsMenuMarkup)
		return
	}

	if len(code) > maxInlineBackupCodeLen {
		filename := fmt.Sprintf("gotdbot-backup-%d.txt", userID)
		caption := fmt.Sprintf(`📎 <b>File Backup Data GotdBot</b>
📋 <code>%d list</code> · 🌐 <code>%d grup</code> · 🧵 <code>%d topic</code> · 📌 <code>%d list topic</code>

Kirimkan kembali file <code>.txt</code> ini di menu <b>⚙️ Settings ⚙️ → ♻️ Restore Data ♻️</b> untuk memulihkan seluruh konfigurasi Anda.`,
			len(cfg.Lists), len(cfg.Groups), len(cfg.Topics), len(cfg.TopicLists))
		if sendErr := sendBackupDocument(ctx, b, chatID, filename, []byte(code), caption); sendErr == nil {
			noticeHTML := fmt.Sprintf(`<h1>📎 Backup Data Dikirim sebagai File</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>📄 Berkas</td><td><code>%s</code></td></tr>
  <tr><td>📋 List / 🌐 Grup</td><td><code>%d list · %d grup</code></td></tr>
  <tr><td>🧵 Topic / 📌 List Topic</td><td><code>%d topic · %d list topic</code></td></tr>
</table>
<blockquote expandable>Karena data konfigurasi Anda cukup besar, kode backup <code>GOTD:B...</code> telah dikirim sebagai berkas <code>.txt</code> di atas. Unggah kembali file tersebut pada menu <b>♻️ Restore Data ♻️</b> untuk memulihkan data.</blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="panel:settings:home">◀️ Kembali</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Data Backup Manager</p>`, html.EscapeString(filename), len(cfg.Lists), len(cfg.Groups), len(cfg.Topics), len(cfg.TopicLists))
			h.sendOrEditBackupView(ctx, b, chatID, editMsgID, noticeHTML, assets.SettingsMenuMarkup)
			return
		}
	}

	if len(code) > maxSingleCardBackupCodeLen {
		summaryHTML := fmt.Sprintf(`<h1>📎 Kode Backup Data Berhasil Dibuat</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>📋 List / 🌐 Grup</td><td><code>%d list · %d grup</code></td></tr>
  <tr><td>🧵 Topic / 📌 List Topic</td><td><code>%d topic · %d list topic</code></td></tr>
</table>
<blockquote expandable>Kode backup dikirim pada pesan terpisah di bawah agar mudah disalin penuh. Tempel kode tersebut di menu <b>♻️ Restore Data ♻️</b>.</blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="panel:settings:home">◀️ Kembali</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Data Backup Manager</p>`, len(cfg.Lists), len(cfg.Groups), len(cfg.Topics), len(cfg.TopicLists))
		h.sendOrEditBackupView(ctx, b, chatID, editMsgID, summaryHTML, assets.SettingsMenuMarkup)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        fmt.Sprintf("<code>%s</code>", html.EscapeString(code)),
			ParseMode:   "HTML",
			ReplyMarkup: assets.SettingsMenuMarkup,
		})
		return
	}

	text := fmt.Sprintf(`<h1>📎 Kode Backup Data Berhasil Dibuat</h1>
<table border="1">
  <tr><th>📌 Properti</th><th>Nilai</th></tr>
  <tr><td>🔑 Kode Backup</td><td><code>%s</code></td></tr>
  <tr><td>📋 List / 🌐 Grup</td><td><code>%d list · %d grup</code></td></tr>
  <tr><td>🧵 Topic / 📌 List Topic</td><td><code>%d topic · %d list topic</code></td></tr>
</table>
<blockquote expandable>ℹ️ Gunakan menu <b>♻️ Restore Data ♻️</b> dan kirim kode di atas untuk memulihkan list, grup, topic &amp; pengaturan ke akun atau slot lain.</blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="panel:settings:home">◀️ Kembali</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Data Backup Manager</p>`,
		html.EscapeString(code), len(cfg.Lists), len(cfg.Groups), len(cfg.Topics), len(cfg.TopicLists),
	)
	h.sendOrEditBackupView(ctx, b, chatID, editMsgID, text, assets.SettingsMenuMarkup)
}

func (h *Handler) sendOrEditBackupView(ctx context.Context, b *botgotd.Bot, chatID int64, editMsgID int, htmlText string, replyMarkup any) {
	if editMsgID > 0 {
		if _, err := b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: editMsgID,
			Text:      htmlText,
			ParseMode: "HTML",
		}); err == nil {
			return
		}
	}
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        htmlText,
		ParseMode:   "HTML",
		ReplyMarkup: replyMarkup,
	})
}

func (h *Handler) processRestoreBackupCode(ctx context.Context, b *botgotd.Bot, chatID, userID int64, rawInput string) bool {
	code := strings.TrimSpace(strings.TrimPrefix(rawInput, "\ufeff"))
	if !IsBackupCodePrefix(code) {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        `<h1>⚠️ Format Kode Tidak Valid</h1><blockquote expandable>Masukkan kode backup yang diawali <code>GOTD:B...</code> atau unggah berkas backup <code>.txt</code> yang valid.</blockquote><hr/><p>GotdBot • Data Backup Manager</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.CancelOnlyMarkup,
		})
		return true
	}

	cfg := h.db.GetUserConfig(ctx, userID)
	policy, enforced := h.getEffectiveTierPolicy(ctx, userID)
	var listCnt, groupCnt, topicCnt, tlistCnt int
	var notes []string

	if strings.HasPrefix(code, tokenPrefixBackup) {
		payload, err := DecodePortableBackup(code)
		if err != nil {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        `<h1>❌ Kode Backup Tidak Valid</h1><blockquote expandable>Kode backup rusak atau tidak dapat dibaca. Pastikan seluruh karakter kode (atau file <code>.txt</code>) utuh dan tidak terpotong.</blockquote><hr/><p>GotdBot • Data Backup Manager</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: assets.CancelOnlyMarkup,
			})
			return true
		}
		listCnt, groupCnt, topicCnt, tlistCnt, notes = applyPortableBackupToConfig(cfg, payload, policy, enforced)
	} else {
		backup, err := h.db.GetDataBackup(ctx, code)
		if err != nil || backup == nil {
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:      chatID,
				Text:        `<h1>❌ Kode Backup Tidak Ditemukan</h1><blockquote expandable>Kode backup yang Anda kirimkan tidak terdaftar dalam sistem.</blockquote><hr/><p>GotdBot • Data Backup Manager</p>`,
				ParseMode:   "HTML",
				ReplyMarkup: assets.CancelOnlyMarkup,
			})
			return true
		}
		lists := append([]int(nil), backup.Lists...)
		groups := append([]databases.GroupEntry(nil), backup.Groups...)
		topics := append([]databases.TopicEntry(nil), backup.Topics...)
		topicLists := append([]databases.TopicListEntry(nil), backup.TopicLists...)
		if enforced && policy.MaxLists > 0 && len(lists) > policy.MaxLists {
			lists = lists[:policy.MaxLists]
			notes = append(notes, fmt.Sprintf("List dibatasi %d item (paket %s)", policy.MaxLists, policy.Tier))
		}
		if enforced && policy.MaxGroups > 0 && len(groups) > policy.MaxGroups {
			groups = groups[:policy.MaxGroups]
			notes = append(notes, fmt.Sprintf("Grup dibatasi %d target (paket %s)", policy.MaxGroups, policy.Tier))
		}
		if enforced && !policy.AllowTopic && (len(topics) > 0 || len(topicLists) > 0) {
			topics = []databases.TopicEntry{}
			topicLists = []databases.TopicListEntry{}
			notes = append(notes, fmt.Sprintf("Topic tidak dipulihkan pada paket %s", policy.Tier))
		}
		cfg.Lists = lists
		cfg.Groups = groups
		cfg.Topics = topics
		cfg.TopicLists = topicLists
		cfg.TopicEnabled = len(topics) > 0
		listCnt = len(lists)
		groupCnt = len(groups)
		topicCnt = len(topics)
		tlistCnt = len(topicLists)
	}

	h.setPanelState(chatID, "")
	_ = h.db.SaveUserConfig(ctx, cfg)
	h.resetTopicForumScan(chatID)

	noteBlock := `<blockquote expandable>Seluruh materi List, target Grup, Topic, dan List Topic telah dipulihkan ke akun aktif ini.</blockquote>`
	if len(notes) > 0 {
		noteBlock = fmt.Sprintf(`<blockquote expandable>⚠️ <b>Catatan Penyesuaian Tier:</b><br/>%s</blockquote>`, html.EscapeString(strings.Join(notes, "; ")))
	}
	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(`<h1>✅ Data Berhasil Dipulihkan</h1>
<table border="1">
  <tr><th>📌 Data</th><th>Jumlah</th></tr>
  <tr><td>📋 List / 🌐 Grup</td><td><code>%d list · %d grup</code></td></tr>
  <tr><td>🧵 Topic / 📌 List Topic</td><td><code>%d topic · %d list topic</code></td></tr>
</table>
%s
<hr/>
<p>GotdBot • Data Backup Manager</p>`,
			listCnt, groupCnt, topicCnt, tlistCnt, noteBlock,
		),
		ParseMode:   "HTML",
		ReplyMarkup: assets.SettingsMenuMarkup,
	})
	return true
}
