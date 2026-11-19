package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
)

func playModeLabel(mode string) string {
	switch databases.NormalizePlayMode(mode) {
	case databases.PlayModeShuffleEven:
		return "⚡ Acak Merata"
	case databases.PlayModeShuffleFree:
		return "❓ Acak Bebas"
	case databases.PlayModeAll:
		return "📣 Semua Post"
	default:
		return "🔁 Bergilir"
	}
}

func (h *Handler) sendOrEditRich(ctx context.Context, b *botgotd.Bot, chatID int64, editMsgID int, htmlText string, markup any) int {
	if editMsgID > 0 {
		if _, err := b.EditMessageText(ctx, &botgotd.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: editMsgID,
			Text:      htmlText,
			ParseMode: "HTML",
		}); err == nil {
			h.setTopicListMsgID(chatID, editMsgID)
			return editMsgID
		}
	}
	params := &botgotd.SendMessageParams{
		ChatID:    chatID,
		Text:      htmlText,
		ParseMode: "HTML",
	}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	msg, err := b.SendMessage(ctx, params)
	if err == nil && msg != nil {
		if editMsgID > 0 && msg.ID != editMsgID {
			h.deleteBotMessages(ctx, b, chatID, []int{editMsgID})
		}
		h.setTopicListMsgID(chatID, msg.ID)
		return msg.ID
	}
	return 0
}

func (h *Handler) ShowListTopic(ctx context.Context, b *botgotd.Bot, chatID, userID int64, editMsgID int) {
	h.setPanelState(chatID, "")
	h.setTopicPromptMsgID(chatID, 0)
	lists := h.db.GetTopicLists(ctx, userID)

	var sb strings.Builder
	sb.WriteString("<h1>📌 List Topic</h1>\n")
	fmt.Fprintf(&sb, "<table border=\"1\">\n  <tr><th>📊 Total List</th><td><code>%d/%d</code></td></tr>\n</table>\n", len(lists), databases.MaxTopicLists)

	if len(lists) == 0 {
		sb.WriteString("<blockquote expandable>Belum ada List Topic. Klik <b>➕ Buat List</b> untuk membuat list khusus topic forum.</blockquote>\n")
	} else {
		sb.WriteString("<table border=\"1\">\n")
		sb.WriteString("  <tr><th>📂 Nama</th><th>📄 Post</th><th>🔁 Putar</th><th>🔗 Topic</th></tr>\n")
		for _, tl := range lists {
			fmt.Fprintf(&sb, "  <tr><td><b>%s</b></td><td><code>%d post</code></td><td>%s</td><td><code>%d topic</code></td></tr>\n",
				html.EscapeString(tl.Name), len(tl.Posts), playModeLabel(tl.PlayMode), len(tl.LinkedTopics))
		}
		sb.WriteString("</table>\n")
	}

	sb.WriteString("<blockquote expandable>ℹ️ Topic yang ditautkan ke List Topic hanya menerima post list itu; topic lain tetap menerima list utama. Tautkan lewat <b>🔗 Hubungkan Topic</b>.</blockquote>\n")

	for start := 0; start < len(lists); start += 2 {
		sb.WriteString("<tg-button-row>\n")
		for i := start; i < start+2 && i < len(lists); i++ {
			tl := lists[i]
			fmt.Fprintf(&sb, `  <tg-button type="callback_data" data="panel:tlist:open:%s">📂 %s · %d</tg-button>`+"\n", tl.Name, html.EscapeString(tl.Name), len(tl.Posts))
		}
		sb.WriteString("</tg-button-row>\n")
	}
	sb.WriteString("<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:tlist:create\" style=\"primary\">➕ Buat List</tg-button>\n</tg-button-row>\n")
	sb.WriteString("<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:tlist:link\">🔗 Hubungkan Topic</tg-button>\n</tg-button-row>\n")
	sb.WriteString("<hr/>\n<p>GotdBot • Topic List Manager</p>")

	var markup any
	if editMsgID == 0 {
		markup = assets.GrupListMenuMarkup
	}
	h.sendOrEditRich(ctx, b, chatID, editMsgID, sb.String(), markup)
}

func (h *Handler) renderTopicListDetail(ctx context.Context, b *botgotd.Bot, chatID, userID int64, editMsgID int, name string) {
	h.setPanelState(chatID, "")
	h.setTopicPromptMsgID(chatID, 0)
	tl := h.db.GetTopicList(ctx, userID, name)
	if tl == nil {
		h.ShowListTopic(ctx, b, chatID, userID, editMsgID)
		return
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<h1>📂 List Topic: %s</h1>\n", html.EscapeString(tl.Name))
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString("  <tr><th>📌 Properti</th><th>Nilai</th></tr>\n")
	fmt.Fprintf(&sb, "  <tr><td>🏷️ Nama List</td><td><code>%s</code></td></tr>\n", html.EscapeString(tl.Name))
	fmt.Fprintf(&sb, "  <tr><td>📄 Jumlah Post</td><td><code>%d/%d post</code></td></tr>\n", len(tl.Posts), databases.MaxTopicListPosts)
	fmt.Fprintf(&sb, "  <tr><td>🔁 Cara Putar</td><td>%s</td></tr>\n", playModeLabel(tl.PlayMode))
	fmt.Fprintf(&sb, "  <tr><td>🔗 Topic Tertaut</td><td><code>%d topic</code></td></tr>\n", len(tl.LinkedTopics))
	sb.WriteString("</table>\n")

	if len(tl.LinkedTopics) > 0 {
		sb.WriteString("<blockquote expandable><b>🔗 Daftar Topic Tertaut:</b>\n")
		for i, lk := range tl.LinkedTopics {
			gTitle := lk.GroupTitle
			if gTitle == "" {
				gTitle = strconv.FormatInt(lk.ChatID, 10)
			}
			tTitle := lk.TopicTitle
			if tTitle == "" {
				tTitle = fmt.Sprintf("Topic #%d", lk.TopicID)
			}
			fmt.Fprintf(&sb, "%d. <b>%s</b> └ %s\n", i+1, html.EscapeString(gTitle), html.EscapeString(tTitle))
		}
		sb.WriteString("</blockquote>\n")
	}

	fmt.Fprintf(&sb, "<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:tlist:addpost:%s\" style=\"primary\">➕ Tambah Post</tg-button>\n  <tg-button type=\"callback_data\" data=\"panel:tlist:posts:%s\">🔍 Lihat Post</tg-button>\n</tg-button-row>\n", tl.Name, tl.Name)
	fmt.Fprintf(&sb, "<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:tlist:mode:%s\">🔁 Cara Putar</tg-button>\n  <tg-button type=\"callback_data\" data=\"panel:tlist:link:%s\">🔗 Hubungkan Topic</tg-button>\n</tg-button-row>\n", tl.Name, tl.Name)
	fmt.Fprintf(&sb, "<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:tlist:delete:%s\" style=\"danger\">🗑️ Hapus List Topic</tg-button>\n</tg-button-row>\n", tl.Name)
	sb.WriteString("<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:tlist:home\">◀️ Kembali ◀️</tg-button>\n</tg-button-row>\n")
	sb.WriteString("<hr/>\n<p>GotdBot • Topic List Manager</p>")

	msgID := h.sendOrEditRich(ctx, b, chatID, editMsgID, sb.String(), nil)
	if msgID > 0 {
		h.trackDetailMessageIDs(ctx, chatID, []int{msgID})
	}
}

func (h *Handler) renderTopicListMode(ctx context.Context, b *botgotd.Bot, chatID, userID int64, editMsgID int, name string) {
	tl := h.db.GetTopicList(ctx, userID, name)
	if tl == nil {
		h.ShowListTopic(ctx, b, chatID, userID, editMsgID)
		return
	}
	cur := databases.NormalizePlayMode(tl.PlayMode)
	styleFor := func(m string) string {
		if cur == m {
			return ` style="primary"`
		}
		return ""
	}

	text := fmt.Sprintf(`<h1>🔁 Cara Putar</h1>
<table border="1">
  <tr><th>📌 List Topic</th><td><code>%s</code></td></tr>
  <tr><th>⚙️ Sekarang</th><td><b>%s</b></td></tr>
</table>
<blockquote expandable>• <b>🔁 Bergilir:</b> post 1, 2, 3, lalu ulang (bawaan)<br/>
• <b>⚡ Acak Merata:</b> urutan acak, setiap post sekali per siklus<br/>
• <b>❓ Acak Bebas:</b> acak, bisa berulang<br/>
• <b>📣 Semua Post:</b> setiap putaran SEMUA post dikirim ke topic itu</blockquote>
<blockquote expandable>⚠️ <b>Peringatan:</b> Semua Post mengirim sampai 20 post per topic setiap putaran: risiko FloodWait dan dianggap spam.</blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="panel:tlist:setmode:%s:sequential"%s>🔁 Bergilir</tg-button>
</tg-button-row>
<tg-button-row>
  <tg-button type="callback_data" data="panel:tlist:setmode:%s:shuffle_even"%s>⚡ Acak Merata</tg-button>
</tg-button-row>
<tg-button-row>
  <tg-button type="callback_data" data="panel:tlist:setmode:%s:shuffle_free"%s>❓ Acak Bebas</tg-button>
</tg-button-row>
<tg-button-row>
  <tg-button type="callback_data" data="panel:tlist:setmode:%s:all"%s>📣 Semua Post</tg-button>
</tg-button-row>
<tg-button-row>
  <tg-button type="callback_data" data="panel:tlist:open:%s">◀️ Kembali ◀️</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Topic List Manager</p>`,
		html.EscapeString(tl.Name), playModeLabel(cur),
		tl.Name, styleFor(databases.PlayModeSequential),
		tl.Name, styleFor(databases.PlayModeShuffleEven),
		tl.Name, styleFor(databases.PlayModeShuffleFree),
		tl.Name, styleFor(databases.PlayModeAll),
		tl.Name,
	)

	h.sendOrEditRich(ctx, b, chatID, editMsgID, text, nil)
}

func (h *Handler) StartCreateTopicList(ctx context.Context, b *botgotd.Bot, chatID int64, editMsgID int) {
	h.setPanelState(chatID, "tlist_create_name")
	text := `<h1>➕ Buat List Topic</h1>
<blockquote expandable>Ketik nama list: huruf kecil, angka, titik, minus, atau garis bawah, tanpa spasi, maks. 24 karakter (mis. <code>makanan</code>).</blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="panel:tlist:home">◀️ Kembali ◀️</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Topic List Manager</p>`
	msgID := h.sendOrEditRich(ctx, b, chatID, editMsgID, text, nil)
	if msgID > 0 {
		h.setTopicPromptMsgID(chatID, msgID)
	}
}

func (h *Handler) handleTopicListCreateInput(ctx context.Context, b *botgotd.Bot, chatID, userID int64, text string) bool {
	name := strings.TrimSpace(text)
	editID := h.getTopicListEditID(chatID)
	if !topicListNameRegex.MatchString(name) {
		msgID := h.sendOrEditRich(ctx, b, chatID, editID, `<h1>⚠️ Nama List Tidak Valid</h1>
<blockquote expandable>Gunakan hanya huruf kecil (<code>a-z</code>), angka (<code>0-9</code>), titik (<code>.</code>), minus (<code>-</code>), atau garis bawah (<code>_</code>), tanpa spasi, maksimal 24 karakter.</blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="panel:tlist:home">◀️ Kembali ◀️</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Topic List Manager</p>`, nil)
		if msgID > 0 {
			h.setTopicPromptMsgID(chatID, msgID)
		}
		return true
	}
	if err := h.db.CreateTopicList(ctx, userID, name); err != nil {
		msgID := h.sendOrEditRich(ctx, b, chatID, editID, fmt.Sprintf(`<h1>⚠️ Gagal Membuat List Topic</h1>
<blockquote expandable>%s</blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="panel:tlist:home">◀️ Kembali ◀️</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Topic List Manager</p>`, html.EscapeString(err.Error())), nil)
		if msgID > 0 {
			h.setTopicPromptMsgID(chatID, msgID)
		}
		return true
	}
	h.setPanelState(chatID, "")
	h.setTopicPromptMsgID(chatID, 0)
	h.renderTopicListDetail(ctx, b, chatID, userID, editID, name)
	return true
}

func (h *Handler) handleTopicListAddPostText(ctx context.Context, b *botgotd.Bot, chatID, userID int64, listName, text string, message *botgotd.Message) bool {
	if message == nil || message.MediaGroupID == "" {
		h.setPanelState(chatID, "")
	}
	if h.broker == nil {
		return true
	}
	editID := h.getTopicListEditID(chatID)
	payload := models.PToService{
		ChatID:   chatID,
		ThreadID: editID,
		UserID:   userID,
		OwnerID:  userID,
		Type:     "cache_topic_list_post",
		TaskID:   listName,
		Text:     text,
		Entities: panelEntities(message),
	}
	if err := h.broker.To(ctx, "to:ubot:command", payload); err != nil {
		msgID := h.sendOrEditRich(ctx, b, chatID, editID, fmt.Sprintf(`<h1>❌ Gagal Menyimpan Post</h1><blockquote expandable>Broker userbot tidak merespons.</blockquote><tg-button-row><tg-button type="callback_data" data="panel:tlist:open:%s">◀️ Kembali ◀️</tg-button></tg-button-row><hr/><p>GotdBot • Topic List Manager</p>`, listName), nil)
		if msgID > 0 {
			h.setTopicPromptMsgID(chatID, msgID)
		}
		return true
	}
	msgID := h.sendOrEditRich(ctx, b, chatID, editID, fmt.Sprintf(`<h1>⏳ Menyimpan Post</h1><blockquote expandable>Mengamankan post ke List Topic <code>%s</code>...</blockquote><hr/><p>GotdBot • Topic List Manager</p>`, html.EscapeString(listName)), nil)
	if msgID > 0 {
		h.setTopicPromptMsgID(chatID, msgID)
	}
	return true
}

func (h *Handler) StartLinkTopic(ctx context.Context, b *botgotd.Bot, chatID, userID int64, targetList string) {
	h.clearPanelPrompt(ctx, b, chatID)
	forums, useFastPath, wantsBg, totalGroups := h.cachedForumGroupsFromDB(ctx, chatID, userID)
	if h.db != nil && totalGroups == 0 {
		_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: `<h1>🔗 Hubungkan Topic</h1>
<blockquote expandable>Tidak ditemukan grup bertipe <b>Forum</b> di daftar grup target Anda. Pastikan Anda sudah menambahkan grup forum melalui <b>📝 Tambah Grup 📝</b>.</blockquote>
<hr/>
<p>GotdBot • Topic List Manager</p>`,
			ParseMode:   "HTML",
			ReplyMarkup: assets.GrupListMenuMarkup,
		})
		return
	}
	if useFastPath {
		h.topicMu.Lock()
		sess := h.ensureTopicSessionLocked(chatID)
		sess.ForumGroups = forums
		sess.LinkTargetList = targetList
		h.topicMu.Unlock()
		h.setPanelState(chatID, "tlist_link_group")
		h.renderLinkGroupSelect(ctx, b, chatID, forums)
		if wantsBg {
			h.triggerBgForumRefresh(ctx, chatID, userID)
		}
		return
	}
	if h.broker == nil {
		return
	}
	taskID := "link_topic"
	if targetList != "" {
		taskID = "link_topic:" + targetList
	}
	h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: `<h1>🔗 Hubungkan Topic</h1>
<blockquote expandable>⏳ Memuat daftar grup forum dari akun userbot Anda...</blockquote>`,
		ParseMode: "HTML",
	})
	_ = h.broker.To(ctx, "to:ubot:command", models.PToService{
		ChatID:  chatID,
		UserID:  userID,
		OwnerID: userID,
		Type:    "fetch_forum_groups",
		TaskID:  taskID,
	})
}

func (h *Handler) renderLinkGroupSelect(ctx context.Context, b *botgotd.Bot, chatID int64, groups []topicForumGroup) {
	var sb strings.Builder
	sb.WriteString("<h1>🔗 Hubungkan Topic</h1>\n")
	sb.WriteString("<p>Pilih grup (list utama) yang topicnya ingin ditautkan:</p>\n")
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString("  <tr><th>#</th><th>🏷️ Grup Forum</th></tr>\n")
	for i, g := range groups {
		fmt.Fprintf(&sb, "  <tr><td><code>%d</code></td><td><b>%s</b></td></tr>\n", i+1, html.EscapeString(g.Title))
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

func (h *Handler) handleTopicListLinkGroupInput(ctx context.Context, b *botgotd.Bot, chatID, userID int64, text string) bool {
	h.topicMu.Lock()
	sess := h.ensureTopicSessionLocked(chatID)
	forumGroups := append([]topicForumGroup(nil), sess.ForumGroups...)
	targetList := sess.LinkTargetList
	h.topicMu.Unlock()
	idx, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || idx < 1 || idx > len(forumGroups) {
		_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>⚠️ Pilihan Grup Tidak Valid</h1>
<blockquote expandable>Pilih nomor antara <code>1</code> sampai <code>%d</code> atau tekan <b>⬅️ Batal ⬅️</b>.</blockquote>`, len(forumGroups)),
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
	taskID := "link_topic"
	if targetList != "" {
		taskID = "link_topic:" + targetList
	}
	h.sendProgress(ctx, b, chatID, &botgotd.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(`<h1>🔗 Hubungkan Topic</h1>
<blockquote expandable>⏳ Mengambil daftar topic dari grup <b>%s</b>...</blockquote>`, html.EscapeString(selected.Title)),
		ParseMode: "HTML",
	})
	if h.broker != nil {
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{
			ChatID:  chatID,
			UserID:  userID,
			OwnerID: userID,
			Type:    "fetch_group_topics",
			TaskID:  taskID,
			Text:    strconv.FormatInt(selected.ChatID, 10),
		})
	}
	return true
}

func (h *Handler) renderLinkTopicSelect(ctx context.Context, b *botgotd.Bot, chatID, userID int64, payload topicForumResponse) {
	lists := h.db.GetTopicLists(ctx, userID)
	linkedTo := make(map[int]string)
	for _, tl := range lists {
		for _, lk := range tl.LinkedTopics {
			if lk.ChatID == payload.ChatID {
				linkedTo[lk.TopicID] = tl.Name
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("<h1>🔗 Pilih Topic untuk Ditautkan</h1>\n")
	fmt.Fprintf(&sb, "<table border=\"1\">\n  <tr><th>🏷️ Grup</th><td><b>%s</b></td></tr>\n</table>\n", html.EscapeString(payload.GroupTitle))
	sb.WriteString("<p>Ketik nomor topic yang ingin ditautkan; beberapa sekaligus bisa (mis. <code>1 2</code> atau <code>1-3</code>).</p>\n")
	sb.WriteString("<table border=\"1\">\n")
	sb.WriteString("  <tr><th>#</th><th>🧵 Nama Topic</th><th>🔗 Tautan</th></tr>\n")
	for i, t := range payload.Topics {
		badge := "List Utama"
		if ln, ok := linkedTo[t.ID]; ok {
			badge = "📂 <code>" + html.EscapeString(ln) + "</code>"
		}
		fmt.Fprintf(&sb, "  <tr><td><code>%d</code></td><td><b>%s</b></td><td>%s</td></tr>\n", i+1, html.EscapeString(t.Title), badge)
	}
	sb.WriteString("</table>\n")
	sb.WriteString("<hr/>\n<p>GotdBot • Topic List Manager</p>")

	msg, _ := b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: topicSelectReplyMarkup(len(payload.Topics), false),
	})
	if msg != nil && msg.ID > 0 {
		h.setPanelPrompt(chatID, msg.ID)
	}
}

func (h *Handler) handleTopicListLinkTopicInput(ctx context.Context, b *botgotd.Bot, chatID, userID int64, text string) bool {
	sess := h.getTopicSession(chatID)
	indices, err := parseTopicIndices(text, len(sess.Topics), databases.MaxTopicsPerGroup)
	if err != nil {
		_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(`<h1>⚠️ Input Topic Tidak Valid</h1>
<blockquote expandable>%s</blockquote>`, html.EscapeString(err.Error())),
			ParseMode:   "HTML",
			ReplyMarkup: topicSelectReplyMarkup(len(sess.Topics), false),
		})
		return true
	}
	sort.Ints(indices)
	links := make([]databases.TopicLink, 0, len(indices))
	for _, idx := range indices {
		t := sess.Topics[idx-1]
		links = append(links, databases.TopicLink{
			ChatID:     sess.SelectedGroup.ChatID,
			GroupTitle: sess.SelectedGroup.Title,
			TopicID:    t.ID,
			TopicTitle: t.Title,
		})
	}

	h.clearPanelPrompt(ctx, b, chatID)
	h.setPanelState(chatID, "")

	if sess.LinkTargetList != "" {
		targetName := sess.LinkTargetList
		sess.LinkTargetList = ""
		_ = h.db.LinkTopicsToList(ctx, userID, targetName, links)
		h.sendLinkSuccessRich(ctx, b, chatID, targetName, links)
		return true
	}

	sess.PendingLinks = links
	lists := h.db.GetTopicLists(ctx, userID)
	var sb strings.Builder
	sb.WriteString("<h1>🔗 Pilih List Topic Tujuan</h1>\n")
	sb.WriteString("<blockquote expandable><b>Topic terpilih:</b>\n")
	for i, lk := range links {
		fmt.Fprintf(&sb, "%d. <b>%s</b> └ %s\n", i+1, html.EscapeString(lk.GroupTitle), html.EscapeString(lk.TopicTitle))
	}
	sb.WriteString("</blockquote>\n")
	for start := 0; start < len(lists); start += 2 {
		sb.WriteString("<tg-button-row>\n")
		for i := start; i < start+2 && i < len(lists); i++ {
			tl := lists[i]
			fmt.Fprintf(&sb, `  <tg-button type="callback_data" data="panel:tlist:bind:%s" style="primary">📂 %s</tg-button>`+"\n", tl.Name, html.EscapeString(tl.Name))
		}
		sb.WriteString("</tg-button-row>\n")
	}
	sb.WriteString("<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:tlist:unbind\" style=\"danger\">🔓 Lepas Tautan (List Utama)</tg-button>\n</tg-button-row>\n")
	sb.WriteString("<tg-button-row>\n  <tg-button type=\"callback_data\" data=\"panel:tlist:home\">◀️ Kembali ◀️</tg-button>\n</tg-button-row>\n")
	sb.WriteString("<hr/>\n<p>GotdBot • Topic List Manager</p>")

	_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: assets.GrupListMenuMarkup,
	})
	return true
}

func (h *Handler) sendLinkSuccessRich(ctx context.Context, b *botgotd.Bot, chatID int64, listName string, links []databases.TopicLink) {
	title := "✅ Topic Berhasil Ditautkan"
	targetLabel := fmt.Sprintf("📂 <code>%s</code>", html.EscapeString(listName))
	if listName == "" {
		title = "🔓 Tautan Topic Dilepas"
		targetLabel = "📋 List Utama"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "<h1>%s</h1>\n", title)
	fmt.Fprintf(&sb, "<table border=\"1\">\n  <tr><th>🎯 Tujuan</th><td>%s</td></tr>\n  <tr><th>🧵 Total Topic</th><td><code>%d topic</code></td></tr>\n</table>\n", targetLabel, len(links))
	sb.WriteString("<blockquote expandable>")
	for i, lk := range links {
		fmt.Fprintf(&sb, "%d. <b>%s</b> └ %s\n", i+1, html.EscapeString(lk.GroupTitle), html.EscapeString(lk.TopicTitle))
	}
	sb.WriteString("</blockquote>\n<hr/>\n<p>GotdBot • Topic List Manager</p>")

	_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        sb.String(),
		ParseMode:   "HTML",
		ReplyMarkup: assets.GrupListMenuMarkup,
	})
}

func (h *Handler) handleTopicListCallback(ctx context.Context, b *botgotd.Bot, chatID int64, messageID int, cb *botgotd.CallbackQuery) {
	effectiveID := h.GetEffectiveUserID(chatID, cb.From.ID)
	parts := strings.Split(cb.Data, ":")
	if len(parts) < 3 {
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		return
	}
	action := parts[2]
	switch action {
	case "home":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.ShowListTopic(ctx, b, chatID, effectiveID, messageID)

	case "create":
		if len(h.db.GetTopicLists(ctx, effectiveID)) >= databases.MaxTopicLists {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
				CallbackQueryID: cb.ID,
				Text:            "Maksimal 20 List Topic tercapai.",
				ShowAlert:       true,
			})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.StartCreateTopicList(ctx, b, chatID, messageID)

	case "open":
		if len(parts) < 4 {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.renderTopicListDetail(ctx, b, chatID, effectiveID, messageID, parts[3])

	case "addpost":
		if len(parts) < 4 {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
			return
		}
		name := parts[3]
		tl := h.db.GetTopicList(ctx, effectiveID, name)
		if tl == nil {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "List Topic tidak ditemukan.", ShowAlert: true})
			return
		}
		if len(tl.Posts) >= databases.MaxTopicListPosts {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Maksimal 20 post per List Topic.", ShowAlert: true})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.setPanelState(chatID, "tlist_add_post:"+name)
		promptText := fmt.Sprintf(`<h1>➕ Tambah Post</h1>
<table border="1">
  <tr><th>📂 List Topic</th><td><code>%s</code></td></tr>
  <tr><th>📄 Slot Post</th><td><code>%d/%d</code></td></tr>
</table>
<blockquote expandable>Kirim post-nya sekarang (teks, foto, video, dokumen, atau album). Aturannya sama dengan list utama.</blockquote>
<tg-button-row>
  <tg-button type="callback_data" data="panel:tlist:open:%s">◀️ Kembali ◀️</tg-button>
</tg-button-row>
<hr/>
<p>GotdBot • Topic List Manager</p>`, html.EscapeString(name), len(tl.Posts), databases.MaxTopicListPosts, name)
		msgID := h.sendOrEditRich(ctx, b, chatID, messageID, promptText, nil)
		if msgID > 0 {
			h.setTopicPromptMsgID(chatID, msgID)
		}

	case "posts":
		if len(parts) < 4 {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.setTopicListMsgID(chatID, messageID)
		if h.broker != nil {
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{
				ChatID:   chatID,
				ThreadID: messageID,
				UserID:   effectiveID,
				OwnerID:  effectiveID,
				Type:     "get_topic_list_posts",
				TaskID:   parts[3],
			})
		}

	case "postdetail":
		if len(parts) < 5 {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.setTopicListMsgID(chatID, messageID)
		if h.broker != nil {
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{
				ChatID:   chatID,
				ThreadID: messageID,
				UserID:   effectiveID,
				OwnerID:  effectiveID,
				Type:     "get_topic_list_post_detail",
				TaskID:   parts[3],
				Text:     parts[4],
			})
		}

	case "delpost":
		if len(parts) < 5 {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
			return
		}
		name := parts[3]
		idx, err := strconv.Atoi(parts[4])
		if err != nil {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Nomor post tidak valid.", ShowAlert: true})
			return
		}
		if _, err := h.db.DeleteTopicListPostByIndex(ctx, effectiveID, name, idx); err != nil {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Post tidak ditemukan.", ShowAlert: true})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "✅ Post berhasil dihapus"})
		h.deleteOtherDetailMessages(ctx, b, chatID, messageID)
		h.setTopicListMsgID(chatID, messageID)
		if h.broker != nil {
			_ = h.broker.To(ctx, "to:ubot:command", models.PToService{
				ChatID:   chatID,
				ThreadID: messageID,
				UserID:   effectiveID,
				OwnerID:  effectiveID,
				Type:     "get_topic_list_posts",
				TaskID:   name,
			})
		}

	case "mode":
		if len(parts) < 4 {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		h.renderTopicListMode(ctx, b, chatID, effectiveID, messageID, parts[3])

	case "setmode":
		if len(parts) < 5 {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
			return
		}
		name := parts[3]
		mode := databases.NormalizePlayMode(parts[4])
		if err := h.db.SetTopicListPlayMode(ctx, effectiveID, name, mode); err != nil {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "List Topic tidak ditemukan.", ShowAlert: true})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "✅ Cara putar: " + playModeLabel(mode),
		})
		h.renderTopicListMode(ctx, b, chatID, effectiveID, messageID, name)

	case "link":
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
		targetList := ""
		if len(parts) >= 4 {
			targetList = parts[3]
		}
		h.StartLinkTopic(ctx, b, chatID, effectiveID, targetList)

	case "bind":
		if len(parts) < 4 {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
			return
		}
		name := parts[3]
		sess := h.getTopicSession(chatID)
		links := sess.PendingLinks
		sess.PendingLinks = nil
		if len(links) == 0 {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Sesi tautan kadaluarsa, ulangi 🔗 Hubungkan Topic.", ShowAlert: true})
			return
		}
		if err := h.db.LinkTopicsToList(ctx, effectiveID, name, links); err != nil {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: err.Error(), ShowAlert: true})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "✅ Topic berhasil ditautkan"})
		if messageID > 0 {
			h.deleteBotMessages(ctx, b, chatID, []int{messageID})
		}
		h.sendLinkSuccessRich(ctx, b, chatID, name, links)

	case "unbind":
		sess := h.getTopicSession(chatID)
		links := sess.PendingLinks
		sess.PendingLinks = nil
		if len(links) == 0 {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "Sesi tautan kadaluarsa.", ShowAlert: true})
			return
		}
		_ = h.db.LinkTopicsToList(ctx, effectiveID, "", links)
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "🔓 Tautan dilepas"})
		if messageID > 0 {
			h.deleteBotMessages(ctx, b, chatID, []int{messageID})
		}
		h.sendLinkSuccessRich(ctx, b, chatID, "", links)

	case "delete":
		if len(parts) < 4 {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
			return
		}
		name := parts[3]
		if err := h.db.DeleteTopicList(ctx, effectiveID, name); err != nil {
			_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "List Topic tidak ditemukan.", ShowAlert: true})
			return
		}
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID, Text: "✅ List Topic berhasil dihapus"})
		h.deleteOtherDetailMessages(ctx, b, chatID, messageID)
		h.ShowListTopic(ctx, b, chatID, effectiveID, messageID)

	default:
		_, _ = b.AnswerCallbackQuery(ctx, &botgotd.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})
	}
}
