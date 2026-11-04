package userbot

import (
	"assterisk/ubot/applications/databases"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func getGroupCache() string {
	if val := strings.TrimSpace(os.Getenv("GROUP_CACHE")); val != "" {
		return val
	}
	return "AiotdCaches"
}

func getNotesCacheTopicID() int {
	if val := strings.TrimSpace(os.Getenv("NOTES_CACHE")); val != "" {
		if id, err := strconv.Atoi(val); err == nil && id > 0 {
			return id
		}
	}
	return 7157
}

func getTeksCacheTopicID() int {
	if val := strings.TrimSpace(os.Getenv("TEKS_CACHE")); val != "" {
		if id, err := strconv.Atoi(val); err == nil && id > 0 {
			return id
		}
	}
	return 3325
}

func getWatermarkCacheTopicID() int {
	if val := strings.TrimSpace(os.Getenv("WATERMARK_CACHE")); val != "" {
		if id, err := strconv.Atoi(val); err == nil && id > 0 {
			return id
		}
	}
	return 6847
}

var (
	noteNameRe       = regexp.MustCompile(`^[\w-]{1,32}$`)
	urlButtonRe      = regexp.MustCompile(`(?i)\[([^\[\]|]+)\|\[(?:https?://[^\]]+)\]\((https?://[^)]+)\)\]`)
	urlButtonSimpRe  = regexp.MustCompile(`(?i)\[([^\[\]|]+)\|(https?://[^\]\s]+)\]`)
	cbButtonRe       = regexp.MustCompile(`\[([^\[\]#]{1,40})#([\w-]{1,32})\]`)
	popButtonRe      = regexp.MustCompile(`\[([^\[\]$]{1,40})\$([\w-]{1,32})\]`)
	htmlTagRe        = regexp.MustCompile(`(?i)</?(?:b|strong|i|em|u|s|strike|code|pre|blockquote|a|tg-spoiler)(?:\s|>)`)
)

type NoteButton struct {
	Kind   string `json:"kind"` // "url", "callback", "popup"
	Label  string `json:"label"`
	Target string `json:"target,omitempty"`
	URL    string `json:"url,omitempty"`
}

type NoteExportPayload struct {
	Version    int                    `json:"version"`
	ExportedAt string                 `json:"exported_at"`
	Notes      []databases.NoteRecord `json:"notes"`
}

func isValidNoteName(name string) bool {
	return noteNameRe.MatchString(name) && !strings.Contains(name, ".") && !strings.HasPrefix(name, "$")
}

func (s *Service) resolveCachePeer(ctx context.Context, client *telegram.Client) (tg.InputPeerClass, error) {
	groupCache := getGroupCache()
	resolved, err := client.API().ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: groupCache})
	if err != nil {
		return nil, err
	}
	return peer.EntitiesFromResult(resolved).ExtractPeer(resolved.Peer)
}

func extractMessageIDFromUpdates(u tg.UpdatesClass) int {
	if u == nil {
		return 0
	}
	switch up := u.(type) {
	case *tg.UpdateShortSentMessage:
		return up.ID
	case *tg.UpdateShortMessage:
		return up.ID
	case *tg.Updates:
		for _, update := range up.Updates {
			switch m := update.(type) {
			case *tg.UpdateNewChannelMessage:
				if m.Message != nil {
					return m.Message.GetID()
				}
			case *tg.UpdateNewMessage:
				if m.Message != nil {
					return m.Message.GetID()
				}
			case *tg.UpdateMessageID:
				return m.ID
			}
		}
	case *tg.UpdatesCombined:
		for _, update := range up.Updates {
			switch m := update.(type) {
			case *tg.UpdateNewChannelMessage:
				if m.Message != nil {
					return m.Message.GetID()
				}
			case *tg.UpdateNewMessage:
				if m.Message != nil {
					return m.Message.GetID()
				}
			case *tg.UpdateMessageID:
				return m.ID
			}
		}
	}
	return 0
}

func extractInputMedia(media tg.MessageMediaClass) tg.InputMediaClass {
	if media == nil {
		return nil
	}
	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		if p, ok := m.Photo.(*tg.Photo); ok {
			return &tg.InputMediaPhoto{
				ID: &tg.InputPhoto{
					ID:            p.ID,
					AccessHash:    p.AccessHash,
					FileReference: p.FileReference,
				},
			}
		}
	case *tg.MessageMediaDocument:
		if d, ok := m.Document.(*tg.Document); ok {
			return &tg.InputMediaDocument{
				ID: &tg.InputDocument{
					ID:            d.ID,
					AccessHash:    d.AccessHash,
					FileReference: d.FileReference,
				},
			}
		}
	}
	return nil
}

func (s *Service) sendReplyToAiotdCaches(ctx context.Context, client *telegram.Client, fromPeer tg.InputPeerClass, reply *tg.Message) (int, error) {
	groupCache := getGroupCache()
	topicID := getNotesCacheTopicID()

	cachePeer, err := s.resolveCachePeer(ctx, client)
	if err != nil {
		return 0, fmt.Errorf("gagal resolve @%s: %w", groupCache, err)
	}

	replyTo := &tg.InputReplyToMessage{
		ReplyToMsgID: topicID,
		TopMsgID:     topicID,
	}

	randomID := time.Now().UnixNano()
	inputMedia := extractInputMedia(reply.Media)

	var updates tg.UpdatesClass

	if inputMedia != nil {
		// Kirim media sebagai balasan (reply) langsung ke pesan topicID (bukan forward)
		req := &tg.MessagesSendMediaRequest{
			Peer:     cachePeer,
			ReplyTo:  replyTo,
			Media:    inputMedia,
			Message:  reply.Message,
			Entities: reply.Entities,
			RandomID: randomID,
		}
		updates, err = client.API().MessagesSendMedia(ctx, req)
	} else if reply.Message != "" {
		// Kirim teks sebagai balasan (reply) langsung ke pesan topicID (bukan forward)
		req := &tg.MessagesSendMessageRequest{
			Peer:     cachePeer,
			ReplyTo:  replyTo,
			Message:  reply.Message,
			Entities: reply.Entities,
			RandomID: randomID,
		}
		updates, err = client.API().MessagesSendMessage(ctx, req)
	} else {
		// Fallback untuk tipe media khusus lainnya
		req := &tg.MessagesForwardMessagesRequest{
			FromPeer:   fromPeer,
			ID:         []int{reply.ID},
			RandomID:   []int64{randomID},
			ToPeer:     cachePeer,
			DropAuthor: true,
		}
		req.SetReplyTo(replyTo)
		updates, err = client.API().MessagesForwardMessages(ctx, req)
	}

	if err != nil {
		return 0, fmt.Errorf("gagal membalas ke @%s/%d: %w", groupCache, topicID, err)
	}

	cachedMsgID := extractMessageIDFromUpdates(updates)
	return cachedMsgID, nil
}

func (s *Service) sendTextToAiotdCachesTopic(ctx context.Context, client *telegram.Client, topicID int, text string) (int, error) {
	groupCache := getGroupCache()
	cachePeer, err := s.resolveCachePeer(ctx, client)
	if err != nil {
		return 0, fmt.Errorf("gagal resolve @%s: %w", groupCache, err)
	}

	replyTo := &tg.InputReplyToMessage{
		ReplyToMsgID: topicID,
		TopMsgID:     topicID,
	}

	randomID := time.Now().UnixNano()
	req := &tg.MessagesSendMessageRequest{
		Peer:     cachePeer,
		ReplyTo:  replyTo,
		Message:  text,
		RandomID: randomID,
	}
	updates, err := client.API().MessagesSendMessage(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("gagal mengirim ke @%s/%d: %w", groupCache, topicID, err)
	}
	cachedMsgID := extractMessageIDFromUpdates(updates)
	return cachedMsgID, nil
}

// extractNoteButtons extracts button directives from text and returns cleaned text and button list.
func extractNoteButtons(rawText string) (string, []NoteButton) {
	type span struct {
		start int
		end   int
		btn   NoteButton
	}
	var spans []span

	for _, m := range urlButtonRe.FindAllStringSubmatchIndex(rawText, -1) {
		spans = append(spans, span{
			start: m[0], end: m[1],
			btn: NoteButton{Kind: "url", Label: strings.TrimSpace(rawText[m[2]:m[3]]), URL: rawText[m[4]:m[5]]},
		})
	}
	for _, m := range urlButtonSimpRe.FindAllStringSubmatchIndex(rawText, -1) {
		spans = append(spans, span{
			start: m[0], end: m[1],
			btn: NoteButton{Kind: "url", Label: strings.TrimSpace(rawText[m[2]:m[3]]), URL: rawText[m[4]:m[5]]},
		})
	}
	for _, m := range cbButtonRe.FindAllStringSubmatchIndex(rawText, -1) {
		spans = append(spans, span{
			start: m[0], end: m[1],
			btn: NoteButton{Kind: "callback", Label: strings.TrimSpace(rawText[m[2]:m[3]]), Target: strings.ToLower(rawText[m[4]:m[5]])},
		})
	}
	for _, m := range popButtonRe.FindAllStringSubmatchIndex(rawText, -1) {
		spans = append(spans, span{
			start: m[0], end: m[1],
			btn: NoteButton{Kind: "popup", Label: strings.TrimSpace(rawText[m[2]:m[3]]), Target: strings.ToLower(rawText[m[4]:m[5]])},
		})
	}

	if len(spans) == 0 {
		return rawText, nil
	}

	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })

	var nonOverlapping []span
	prevEnd := -1
	for _, s := range spans {
		if s.start >= prevEnd {
			nonOverlapping = append(nonOverlapping, s)
			prevEnd = s.end
		}
	}

	var sb strings.Builder
	cursor := 0
	var buttons []NoteButton
	for _, s := range nonOverlapping {
		sb.WriteString(rawText[cursor:s.start])
		buttons = append(buttons, s.btn)
		cursor = s.end
	}
	sb.WriteString(rawText[cursor:])

	return strings.TrimSpace(sb.String()), buttons
}

// saveNoteCommand implements !save <nama> [-cb|-pop]
func (s *Service) saveNoteCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, msg *tg.Message, args string) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return sendUserText(ctx, client, target, "Balas pesan yang ingin disimpan dengan: !save <nama> [-cb|-pop]")
	}
	name := strings.ToLower(fields[0])
	if !isValidNoteName(name) {
		return sendUserText(ctx, client, target, "Nama catatan harus 1–32 karakter alfanumerik, underscore (_), atau minus (-).")
	}

	mode := "text"
	for _, f := range fields[1:] {
		switch strings.ToLower(f) {
		case "-cb":
			mode = "callback"
		case "-pop":
			mode = "popup"
		}
	}

	reply, err := s.replyMessage(ctx, client, msg, target)
	if err != nil || reply == nil {
		return sendUserText(ctx, client, target, "Balas pesan (teks atau media) yang ingin disimpan.")
	}

	rawText := reply.Message
	mediaType := ""
	mediaData := ""

	if reply.Media != nil {
		switch m := reply.Media.(type) {
		case *tg.MessageMediaPhoto:
			mediaType = "photo"
			if p, ok := m.Photo.(*tg.Photo); ok {
				mediaData = fmt.Sprintf("%d:%d", p.ID, p.AccessHash)
			}
		case *tg.MessageMediaDocument:
			mediaType = "document"
			if d, ok := m.Document.(*tg.Document); ok {
				mediaData = fmt.Sprintf("%d:%d:%s", d.ID, d.AccessHash, d.MimeType)
			}
		}
	}

	if rawText == "" && mediaType == "" {
		return sendUserText(ctx, client, target, "Pesan yang dibalas tidak berisi teks atau media yang dapat disimpan.")
	}

	cleanedText, buttons := extractNoteButtons(rawText)
	buttonsJSON := ""
	if len(buttons) > 0 {
		bBytes, _ := json.Marshal(buttons)
		buttonsJSON = string(bBytes)
	}

	// Double backup 1: Kirim balasan (reply) langsung ke pesan NOTES_CACHE di GROUP_CACHE untuk monitoring (bukan forward)
	groupCache := getGroupCache()
	topicID := getNotesCacheTopicID()
	cacheMsgID, cacheErr := s.sendReplyToAiotdCaches(ctx, client, target, reply)
	if cacheErr != nil {
		log.Printf("[NOTES CACHE] gagal membalas ke @%s/%d: %v", groupCache, topicID, cacheErr)
		return sendUserText(ctx, client, target, fmt.Sprintf("Gagal membalas pesan ke @%s/%d: %v. Pastikan akun userbot dapat mengakses channel dan pesan %d valid.", groupCache, topicID, cacheErr, topicID))
	}

	noteRecord := databases.NoteRecord{
		OwnerID:        ownerID,
		Name:           name,
		Text:           cleanedText,
		Buttons:        buttonsJSON,
		Mode:           mode,
		MediaType:      mediaType,
		MediaData:      mediaData,
		CacheMessageID: cacheMsgID,
		CacheChannel:   groupCache,
	}

	if s.sqlite != nil {
		if err := s.sqlite.SaveNote(ctx, noteRecord); err != nil {
			return sendUserText(ctx, client, target, "Gagal menyimpan catatan: "+err.Error())
		}
	}

	// Double backup 2: Replikasi metadata catatan ke MongoDB Cloud Atlas (asinkron)
	if s.db != nil {
		go func(nr databases.NoteRecord) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			filter := bson.M{"owner_id": nr.OwnerID, "name": nr.Name}
			update := bson.M{"$set": bson.M{
				"owner_id":         nr.OwnerID,
				"name":             nr.Name,
				"text":             nr.Text,
				"entities":         nr.Entities,
				"buttons":          nr.Buttons,
				"mode":             nr.Mode,
				"media_type":       nr.MediaType,
				"media_data":       nr.MediaData,
				"cache_message_id": nr.CacheMessageID,
				"cache_channel":   nr.CacheChannel,
				"updated_at":       time.Now(),
			}}
			_, err := s.db.Collection("userbot_notes").UpdateOne(bgCtx, filter, update, options.Update().SetUpsert(true))
			if err != nil {
				log.Printf("[NOTES BACKUP] save %s gagal: %v", nr.Name, err)
			}
		}(noteRecord)
	}

	if s.redis != nil {
		payload := NoteInlinePayload{
			OwnerID: ownerID,
			Name:    name,
			Text:    cleanedText,
			Buttons: buttons,
			Mode:    mode,
		}
		if pBytes, err := json.Marshal(payload); err == nil {
			_ = s.redis.Client.Set(ctx, fmt.Sprintf("note:record:%d:%s", ownerID, name), pBytes, 7*24*time.Hour).Err()
		}
		_ = s.redis.Client.Set(ctx, fmt.Sprintf("note:text:%d:%s", ownerID, name), cleanedText, 7*24*time.Hour).Err()
	}

	return sendUserText(ctx, client, target, fmt.Sprintf("Catatan <code>%s</code> berhasil disimpan (ID Cache: %d).", html.EscapeString(name), cacheMsgID))
}

// NoteInlinePayload defines the data stored in Redis for inline notes
type NoteInlinePayload struct {
	OwnerID int64        `json:"owner_id"`
	Name    string       `json:"name"`
	Text    string       `json:"text"`
	Buttons []NoteButton `json:"buttons"`
	Mode    string       `json:"mode"`
}

func (s *Service) sendInlineNote(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, ownerID int64, note *databases.NoteRecord, buttons []NoteButton) error {
	if s.redis == nil {
		return fmt.Errorf("redis client tidak tersedia")
	}
	refBytes := make([]byte, 12)
	if _, err := rand.Read(refBytes); err != nil {
		return err
	}
	ref := base64.RawURLEncoding.EncodeToString(refBytes)

	text := note.Text
	if strings.TrimSpace(text) == "" {
		text = fmt.Sprintf("Catatan: <b>%s</b>", html.EscapeString(note.Name))
	}

	payload := NoteInlinePayload{
		OwnerID: ownerID,
		Name:    note.Name,
		Text:    text,
		Buttons: buttons,
		Mode:    note.Mode,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if err := s.redis.Client.Set(ctx, "inline:note:"+ref, data, 10*time.Minute).Err(); err != nil {
		return err
	}
	_ = s.redis.Client.Set(ctx, fmt.Sprintf("note:record:%d:%s", ownerID, note.Name), data, 7*24*time.Hour).Err()
	_ = s.redis.Client.Set(ctx, fmt.Sprintf("note:text:%d:%s", ownerID, note.Name), text, 7*24*time.Hour).Err()

	// Pre-cache semua target note yang dirujuk oleh tombol callback/popup untuk perpindahan halaman instan
	for _, b := range buttons {
		if (b.Kind == "callback" || b.Kind == "popup") && b.Target != "" && b.Target != note.Name {
			if s.sqlite != nil {
				if targetNote, _ := s.sqlite.GetNote(ctx, ownerID, b.Target); targetNote != nil {
					var targetButtons []NoteButton
					if targetNote.Buttons != "" {
						_ = json.Unmarshal([]byte(targetNote.Buttons), &targetButtons)
					}
					targetPayload := NoteInlinePayload{
						OwnerID: ownerID,
						Name:    targetNote.Name,
						Text:    targetNote.Text,
						Buttons: targetButtons,
						Mode:    targetNote.Mode,
					}
					if tpBytes, err := json.Marshal(targetPayload); err == nil {
						_ = s.redis.Client.Set(ctx, fmt.Sprintf("note:record:%d:%s", ownerID, targetNote.Name), tpBytes, 7*24*time.Hour).Err()
					}
					_ = s.redis.Client.Set(ctx, fmt.Sprintf("note:text:%d:%s", ownerID, targetNote.Name), targetNote.Text, 7*24*time.Hour).Err()
				}
			}
		}
	}

	query := "note_show ref:" + ref
	return s.sendInlineQuery(ctx, client, target, query)
}

// showNote sends the stored note to the current chat
func (s *Service) showNote(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, name string, noFormat bool) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	name = strings.ToLower(name)
	if !isValidNoteName(name) {
		return sendUserText(ctx, client, target, "Gunakan: !get <nama> atau #<nama>")
	}

	var note *databases.NoteRecord
	if s.sqlite != nil {
		note, _ = s.sqlite.GetNote(ctx, ownerID, name)
	}
	if note == nil {
		return sendUserText(ctx, client, target, fmt.Sprintf("Catatan <code>%s</code> tidak ditemukan.", html.EscapeString(name)))
	}

	var buttons []NoteButton
	if note.Buttons != "" {
		_ = json.Unmarshal([]byte(note.Buttons), &buttons)
	}

	outText := note.Text
	if noFormat {
		return sendUserText(ctx, client, target, outText)
	}

	// Jika note memiliki mode callback/popup tetapi buttons kosong, buat tombol default
	if len(buttons) == 0 {
		if note.Mode == "callback" {
			buttons = append(buttons, NoteButton{Kind: "callback", Label: note.Name, Target: note.Name})
		} else if note.Mode == "popup" {
			buttons = append(buttons, NoteButton{Kind: "popup", Label: note.Name, Target: note.Name})
		}
	}

	// 1. Jika catatan memiliki tombol ATAU disimpan dengan -cb / -pop:
	// Tampilkan via Inline Query bot resmi agar tombol inline muncul (seperti menu !help)
	if len(buttons) > 0 || note.Mode == "callback" || note.Mode == "popup" {
		if inlineErr := s.sendInlineNote(ctx, client, target, ownerID, note, buttons); inlineErr == nil {
			return nil
		} else {
			log.Printf("[NOTES INLINE] gagal kirim note inline: %v, beralih ke fallback pengiriman langsung", inlineErr)
		}
	}

	// 2. Jika pesan tersimpan di cache @GROUP_CACHE (tanpa tombol inline), salin langsung dari cache
	if !noFormat && note.CacheMessageID > 0 {
		if cachePeer, err := s.resolveCachePeer(ctx, client); err == nil {
			req := &tg.MessagesForwardMessagesRequest{
				FromPeer:   cachePeer,
				ID:         []int{note.CacheMessageID},
				RandomID:   []int64{time.Now().UnixNano()},
				ToPeer:     target,
				DropAuthor: true,
			}
			if _, err := client.API().MessagesForwardMessages(ctx, req); err == nil {
				return nil
			}
		}
	}

	// 2. Default: Kirim teks catatan langsung beserta link tombol (jika ada)
	if len(buttons) > 0 {
		var btnLinks []string
		for _, b := range buttons {
			if b.Kind == "url" && b.URL != "" {
				btnLinks = append(btnLinks, fmt.Sprintf("👉 <a href=\"%s\">%s</a>", html.EscapeString(b.URL), html.EscapeString(b.Label)))
			} else if (b.Kind == "callback" || b.Kind == "popup") && b.Target != "" {
				btnLinks = append(btnLinks, fmt.Sprintf("👉 <code>#%s</code> (%s)", html.EscapeString(b.Target), html.EscapeString(b.Label)))
			}
		}
		if len(btnLinks) > 0 {
			outText = outText + "\n\n" + strings.Join(btnLinks, " | ")
		}
	}

	return sendUserText(ctx, client, target, outText)
}

// listNotesCommand implements !lsnote or !notes
func (s *Service) listNotesCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}

	var notes []databases.NoteRecord
	if s.sqlite != nil {
		notes, err = s.sqlite.ListNotes(ctx, ownerID)
		if err != nil {
			return sendUserText(ctx, client, target, "Gagal membaca catatan: "+err.Error())
		}
	}

	if len(notes) == 0 {
		return sendUserText(ctx, client, target, "Belum ada catatan. Balas pesan dengan <code>!save <nama></code>.")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Daftar Catatan</b> (Total: %d)\n\n", len(notes)))
	for i, n := range notes {
		sb.WriteString(fmt.Sprintf("%d. <code>%s</code>\n", i+1, html.EscapeString(n.Name)))
	}

	return sendUserText(ctx, client, target, sb.String())
}

// deleteNoteCommand implements !delnote <nama|nomor>
func (s *Service) deleteNoteCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, args string) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return sendUserText(ctx, client, target, "Format: <code>!delnote <nama></code> atau <code>!delnote <nomor></code>.")
	}
	val := fields[0]

	targetName := strings.ToLower(val)
	if idx, parseErr := strconv.Atoi(val); parseErr == nil && idx > 0 {
		if s.sqlite != nil {
			notes, _ := s.sqlite.ListNotes(ctx, ownerID)
			if idx > len(notes) {
				return sendUserText(ctx, client, target, fmt.Sprintf("Nomor catatan harus antara 1 dan %d.", len(notes)))
			}
			targetName = notes[idx-1].Name
		}
	}

	if !isValidNoteName(targetName) {
		return sendUserText(ctx, client, target, "Nama catatan tidak valid.")
	}

	deleted := false
	if s.sqlite != nil {
		deleted, _ = s.sqlite.DeleteNote(ctx, ownerID, targetName)
	}

	// Async MongoDB backup
	if s.db != nil {
		go func(oid int64, name string) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_notes").DeleteOne(bgCtx, bson.M{"owner_id": oid, "name": name})
		}(ownerID, targetName)
	}

	if !deleted {
		return sendUserText(ctx, client, target, fmt.Sprintf("Catatan <code>%s</code> tidak ditemukan.", html.EscapeString(targetName)))
	}

	return sendUserText(ctx, client, target, fmt.Sprintf("Catatan <code>%s</code> berhasil dihapus.", html.EscapeString(targetName)))
}

// clearNotesCommand implements !clearnote
func (s *Service) clearNotesCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}

	var count int64
	if s.sqlite != nil {
		count, _ = s.sqlite.ClearNotes(ctx, ownerID)
	}

	// Async MongoDB backup
	if s.db != nil {
		go func(oid int64) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_notes").DeleteMany(bgCtx, bson.M{"owner_id": oid})
		}(ownerID)
	}

	return sendUserText(ctx, client, target, fmt.Sprintf("%d catatan berhasil dihapus.", count))
}

// nofmCommand implements !nofm [-html|-md]
func (s *Service) nofmCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, msg *tg.Message, args string) error {
	reply, err := s.replyMessage(ctx, client, msg, target)
	if err != nil || reply == nil {
		return sendUserText(ctx, client, target, "Balas pesan berformat dengan <code>!nofm</code> atau <code>!nofm -md</code>.")
	}

	text := reply.Message
	if text == "" {
		return sendUserText(ctx, client, target, "Pesan yang dibalas tidak memiliki teks.")
	}

	mode := "html"
	if strings.Contains(strings.ToLower(args), "-md") {
		mode = "md"
	}

	var formatted string
	if mode == "md" {
		formatted = convertEntitiesToMarkdown(text, reply.Entities)
	} else {
		formatted = convertEntitiesToHTML(text, reply.Entities)
	}

	// Kirim sebagai block code agar terlihat mentah tanpa di-parse
	sender := s.getSender(client)
	_, sendErr := sender.To(target).StyledText(ctx, styling.Pre(formatted, mode))
	return sendErr
}

// exportNotesCommand implements !exportnote (Tanpa Password)
func (s *Service) exportNotesCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}

	var notes []databases.NoteRecord
	if s.sqlite != nil {
		notes, err = s.sqlite.ListNotes(ctx, ownerID)
		if err != nil {
			return sendUserText(ctx, client, target, "Gagal mengekspor catatan: "+err.Error())
		}
	}

	if len(notes) == 0 {
		return sendUserText(ctx, client, target, "Tidak ada catatan untuk diexport.")
	}

	payload := NoteExportPayload{
		Version:    1,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Notes:      notes,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal membuat format file JSON export.")
	}

	u := uploader.NewUploader(client.API())
	f, err := u.FromBytes(ctx, "gotdbot-notes.json", data)
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal mengunggah file export: "+err.Error())
	}

	doc := &tg.InputMediaUploadedDocument{
		File:     f,
		MimeType: "application/json",
	}

	_, err = client.API().MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
		Peer:     target,
		Media:    doc,
		Message:  fmt.Sprintf("%d catatan berhasil diexport (tanpa password).", len(notes)),
		RandomID: time.Now().UnixNano(),
	})
	return err
}

// importNotesCommand implements !importnote (Tanpa Password)
func (s *Service) importNotesCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, msg *tg.Message) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil {
		return err
	}

	reply, err := s.replyMessage(ctx, client, msg, target)
	if err != nil || reply == nil || reply.Media == nil {
		return sendUserText(ctx, client, target, "Balas file <code>gotdbot-notes.json</code> dengan <code>!importnote</code>.")
	}

	docMedia, ok := reply.Media.(*tg.MessageMediaDocument)
	if !ok {
		return sendUserText(ctx, client, target, "Lampiran pesan yang dibalas bukan file dokumen.")
	}

	doc, ok := docMedia.Document.(*tg.Document)
	if !ok {
		return sendUserText(ctx, client, target, "Dokumen tidak valid.")
	}

	if doc.Size > 2*1024*1024 {
		return sendUserText(ctx, client, target, "Ukuran file import maksimal 2 MB.")
	}

	var buf bytes.Buffer
	// Download dokumen menggunakan downloader gotd
	location := &tg.InputDocumentFileLocation{
		ID:            doc.ID,
		AccessHash:    doc.AccessHash,
		FileReference: doc.FileReference,
	}

	// Download potongan chunks
	offset := int64(0)
	limit := 1024 * 1024
	for {
		req := &tg.UploadGetFileRequest{
			Location: location,
			Offset:   offset,
			Limit:    limit,
		}
		res, err := client.API().UploadGetFile(ctx, req)
		if err != nil {
			return sendUserText(ctx, client, target, "Gagal mengunduh file dokumen: "+err.Error())
		}
		switch f := res.(type) {
		case *tg.UploadFile:
			buf.Write(f.Bytes)
			offset += int64(len(f.Bytes))
			if len(f.Bytes) < limit {
				goto DownloadDone
			}
		case *tg.UploadFileCDNRedirect:
			return sendUserText(ctx, client, target, "File CDN tidak didukung.")
		default:
			goto DownloadDone
		}
	}

DownloadDone:
	var payload NoteExportPayload
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		return sendUserText(ctx, client, target, "File catatan tidak valid atau format JSON salah.")
	}

	if len(payload.Notes) == 0 {
		return sendUserText(ctx, client, target, "File tidak berisi catatan.")
	}

	imported := 0
	groupCache := getGroupCache()
	for _, n := range payload.Notes {
		name := strings.ToLower(n.Name)
		if !isValidNoteName(name) {
			continue
		}
		n.OwnerID = ownerID
		n.Name = name
		if n.CacheChannel == "" && n.CacheMessageID > 0 {
			n.CacheChannel = groupCache
		}
		if s.sqlite != nil {
			if err := s.sqlite.SaveNote(ctx, n); err == nil {
				imported++
				if s.redis != nil {
					var nButtons []NoteButton
					if n.Buttons != "" {
						_ = json.Unmarshal([]byte(n.Buttons), &nButtons)
					}
					nPayload := NoteInlinePayload{
						OwnerID: ownerID,
						Name:    n.Name,
						Text:    n.Text,
						Buttons: nButtons,
						Mode:    n.Mode,
					}
					if npBytes, err := json.Marshal(nPayload); err == nil {
						_ = s.redis.Client.Set(ctx, fmt.Sprintf("note:record:%d:%s", ownerID, n.Name), npBytes, 7*24*time.Hour).Err()
					}
					_ = s.redis.Client.Set(ctx, fmt.Sprintf("note:text:%d:%s", ownerID, n.Name), n.Text, 7*24*time.Hour).Err()
				}
				// Async MongoDB backup per note
				if s.db != nil {
					go func(nr databases.NoteRecord) {
						bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						filter := bson.M{"owner_id": nr.OwnerID, "name": nr.Name}
						update := bson.M{"$set": bson.M{
							"owner_id":         nr.OwnerID,
							"name":             nr.Name,
							"text":             nr.Text,
							"entities":         nr.Entities,
							"buttons":          nr.Buttons,
							"mode":             nr.Mode,
							"media_type":       nr.MediaType,
							"media_data":       nr.MediaData,
							"cache_message_id": nr.CacheMessageID,
							"cache_channel":   nr.CacheChannel,
							"updated_at":       time.Now(),
						}}
						_, err := s.db.Collection("userbot_notes").UpdateOne(bgCtx, filter, update, options.Update().SetUpsert(true))
						if err != nil {
							log.Printf("[NOTES BACKUP] import %s gagal: %v", nr.Name, err)
						}
					}(n)
				}
			}
		}
	}

	return sendUserText(ctx, client, target, fmt.Sprintf("%d catatan berhasil diimport.", imported))
}

// convertEntitiesToHTML converts text with Telegram message entities to HTML syntax
func convertEntitiesToHTML(text string, entities []tg.MessageEntityClass) string {
	if len(entities) == 0 {
		return html.EscapeString(text)
	}

	utf16Text := utf16.Encode([]rune(text))
	type insert struct {
		pos int
		tag string
	}
	var inserts []insert

	for _, e := range entities {
		switch entity := e.(type) {
		case *tg.MessageEntityBold:
			inserts = append(inserts, insert{pos: entity.Offset, tag: "<b>"})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "</b>"})
		case *tg.MessageEntityItalic:
			inserts = append(inserts, insert{pos: entity.Offset, tag: "<i>"})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "</i>"})
		case *tg.MessageEntityCode:
			inserts = append(inserts, insert{pos: entity.Offset, tag: "<code>"})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "</code>"})
		case *tg.MessageEntityPre:
			inserts = append(inserts, insert{pos: entity.Offset, tag: "<pre>"})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "</pre>"})
		case *tg.MessageEntityStrike:
			inserts = append(inserts, insert{pos: entity.Offset, tag: "<s>"})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "</s>"})
		case *tg.MessageEntityUnderline:
			inserts = append(inserts, insert{pos: entity.Offset, tag: "<u>"})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "</u>"})
		case *tg.MessageEntitySpoiler:
			inserts = append(inserts, insert{pos: entity.Offset, tag: "<tg-spoiler>"})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "</tg-spoiler>"})
		case *tg.MessageEntityTextURL:
			inserts = append(inserts, insert{pos: entity.Offset, tag: fmt.Sprintf("<a href=\"%s\">", html.EscapeString(entity.URL))})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "</a>"})
		}
	}

	sort.SliceStable(inserts, func(i, j int) bool {
		if inserts[i].pos == inserts[j].pos {
			return strings.HasPrefix(inserts[i].tag, "</")
		}
		return inserts[i].pos < inserts[j].pos
	})

	var sb strings.Builder
	cursor := 0
	for _, ins := range inserts {
		if ins.pos > cursor {
			if ins.pos <= len(utf16Text) {
				sb.WriteString(html.EscapeString(string(utf16.Decode(utf16Text[cursor:ins.pos]))))
				cursor = ins.pos
			}
		}
		sb.WriteString(ins.tag)
	}
	if cursor < len(utf16Text) {
		sb.WriteString(html.EscapeString(string(utf16.Decode(utf16Text[cursor:]))))
	}

	return sb.String()
}

// convertEntitiesToMarkdown converts text with Telegram message entities to Markdown syntax
func convertEntitiesToMarkdown(text string, entities []tg.MessageEntityClass) string {
	if len(entities) == 0 {
		return text
	}
	utf16Text := utf16.Encode([]rune(text))
	type insert struct {
		pos int
		tag string
	}
	var inserts []insert

	for _, e := range entities {
		switch entity := e.(type) {
		case *tg.MessageEntityBold:
			inserts = append(inserts, insert{pos: entity.Offset, tag: "**"})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "**"})
		case *tg.MessageEntityItalic:
			inserts = append(inserts, insert{pos: entity.Offset, tag: "_"})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "_"})
		case *tg.MessageEntityCode:
			inserts = append(inserts, insert{pos: entity.Offset, tag: "`"})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "`"})
		case *tg.MessageEntityPre:
			inserts = append(inserts, insert{pos: entity.Offset, tag: "```\n"})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "\n```"})
		case *tg.MessageEntityStrike:
			inserts = append(inserts, insert{pos: entity.Offset, tag: "~~"})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "~~"})
		case *tg.MessageEntitySpoiler:
			inserts = append(inserts, insert{pos: entity.Offset, tag: "||"})
			inserts = append(inserts, insert{pos: entity.Offset + entity.Length, tag: "||"})
		}
	}

	sort.SliceStable(inserts, func(i, j int) bool {
		return inserts[i].pos < inserts[j].pos
	})

	var sb strings.Builder
	cursor := 0
	for _, ins := range inserts {
		if ins.pos > cursor {
			if ins.pos <= len(utf16Text) {
				sb.WriteString(string(utf16.Decode(utf16Text[cursor:ins.pos])))
				cursor = ins.pos
			}
		}
		sb.WriteString(ins.tag)
	}
	if cursor < len(utf16Text) {
		sb.WriteString(string(utf16.Decode(utf16Text[cursor:])))
	}

	return sb.String()
}
