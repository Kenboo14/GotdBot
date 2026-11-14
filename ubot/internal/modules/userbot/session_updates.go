package userbot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/bson"
)

func isSessionRevokedError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToUpper(err.Error())

	// Fast exclusion: jika network/I/O error atau timeout, JANGAN PERNAH dianggap session revoked
	if strings.Contains(errStr, "TIMEOUT") ||
		strings.Contains(errStr, "DEADLINE EXCEEDED") ||
		strings.Contains(errStr, "RESET BY PEER") ||
		strings.Contains(errStr, "BROKEN PIPE") ||
		strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "NETWORK IS UNREACHABLE") ||
		strings.Contains(errStr, "CONNECTION REFUSED") ||
		strings.Contains(errStr, "NO SUCH HOST") {
		return false
	}

	// Non-auth Telegram error: JANGAN dianggap session revoked
	if strings.Contains(errStr, "FLOOD_WAIT") ||
		strings.Contains(errStr, "CHAT_WRITE_FORBIDDEN") ||
		strings.Contains(errStr, "PEER_ID_INVALID") ||
		strings.Contains(errStr, "CHANNEL_PRIVATE") ||
		strings.Contains(errStr, "CHAT_ID_INVALID") {
		return false
	}

	// Klasifikasi error autentikasi permanen resmi Telegram MTProto
	return strings.Contains(errStr, "AUTH_KEY_UNREGISTERED") ||
		strings.Contains(errStr, "SESSION_REVOKED") ||
		strings.Contains(errStr, "SESSION_EXPIRED") ||
		strings.Contains(errStr, "USER_DEACTIVATED") ||
		strings.Contains(errStr, "AUTH_KEY_PERM_EMPTY") ||
		strings.Contains(errStr, "AUTH_KEY_DUPLICATED")
}

func (s *Service) handleSessionRevoked(chatID int64, genID uint64, reason error) {
	s.mu.Lock()
	currentGen := s.accountGens[chatID]
	if currentGen == 0 || (genID != 0 && currentGen != genID) {
		s.mu.Unlock()
		return
	}
	delete(s.accountGens, chatID)
	s.accountStates[chatID] = StateSessionInvalid

	client := s.active[chatID]
	ownerID := s.owners[chatID]
	stateReason := "Sesi Telegram dicabut"
	if reason != nil {
		stateReason = reason.Error()
	}
	delete(s.active, chatID)
	delete(s.owners, chatID)
	delete(s.lastUpdate, chatID)
	delete(s.lastVerified, chatID)
	delete(s.verifying, chatID)
	if client != nil {
		delete(s.clientOwners, client)
		delete(s.senderCache, client)
	}
	cancel := s.activeCancels[chatID]
	delete(s.activeCancels, chatID)
	delete(s.accountCtxs, chatID)
	s.mu.Unlock()

	slog.Error("session revoked from telegram",
		slog.Int64("chat_id", chatID),
		slog.Int64("owner_id", ownerID),
		slog.Uint64("gen_id", genID),
		slog.String("reason", stateReason),
	)

	s.publishSessionState(chatID, ownerID, StateSessionInvalid, stateReason)

	if cancel != nil {
		cancel()
	}

	if s.sqlite != nil {
		_ = s.sqlite.DeleteSession(context.Background(), chatID)
	}
	if s.db != nil {
		_, _ = s.db.Collection("userbot_sessions").DeleteOne(context.Background(), bson.M{"_id": chatID})
	}

	s.mu.Lock()
	s.accountStates[chatID] = StateStopped
	s.mu.Unlock()

	s.reply(context.Background(), chatID, `<h1>⚠️ Sesi Userbot Terlepas</h1>
<blockquote expandable>Koneksi MTProto userbot Anda terputus atau sesi telah dicabut dari aplikasi Telegram.</blockquote>
<hr/>
<p>Ketik atau tekan <b>/start</b> untuk menyambungkan kembali akun Anda.</p>`, "session_revoked")
}

func (s *Service) handleUserbotMessage(ctx context.Context, chatID int64, client *telegram.Client, entities tg.Entities, update *tg.UpdateNewMessage) error {
	msg, ok := update.Message.(*tg.Message)
	if !ok {
		return nil
	}
	prefix := "!"
	s.mu.Lock()
	if p, ok := s.prefixes[chatID]; ok {
		prefix = p
	}
	s.mu.Unlock()

	// 0. Cek trigger hashtag note: misal "#nama" (diperbolehkan tanpa prefix perintah)
	if strings.HasPrefix(msg.Message, "#") && len(msg.Message) > 1 && !strings.ContainsAny(msg.Message, " \t\r\n") {
		hashName := strings.TrimPrefix(msg.Message, "#")
		if isValidNoteName(hashName) && s.commandAllowed(ctx, client, msg) {
			peerID, err := peer.EntitiesFromUpdate(entities).ExtractPeer(msg.PeerID)
			if err == nil {
				return s.showNote(ctx, client, peerID, hashName, false)
			}
		}
	}

	// 1. Jika prefix aktif dan pesan tidak diawali prefix, tolak langsung dalam < 10 nanodetik
	if prefix != "" {
		if !strings.HasPrefix(msg.Message, prefix) {
			return nil
		}
	} else {
		// 2. Jika prefix == "none" (tanpa prefix):
		if !msg.Out {
			ownerID := s.owners[chatID]
			senderID := int64(0)
			if sender, ok := msg.FromID.(*tg.PeerUser); ok {
				senderID = sender.UserID
			}
			if senderID == 0 || !s.isSudo(ctx, ownerID, senderID) {
				return nil
			}
		}
	}
	name, args, ok := parseUserCommand(msg.Message, prefix)
	if !ok {
		return nil
	}
	if !s.commandAllowed(ctx, client, msg) {
		return nil
	}
	peerID, err := peer.EntitiesFromUpdate(entities).ExtractPeer(msg.PeerID)
	if err != nil {
		return err
	}
	if name == "setp" {
		return s.handleSetPrefix(ctx, client, chatID, peerID, args)
	}
	if name == "help" {
		if err := s.sendInlineHelp(ctx, client, peerID); err != nil {
			log.Printf("[userbot %d] !help inline gagal: %v", msg.ID, err)
			_, sendErr := s.getSender(client).To(peerID).Text(ctx, "Help gagal. Periksa koneksi bot inline dan status sesi userbot.")
			return sendErr
		}
		return nil
	}
	if name == "server" {
		if err := s.sendInlineQuery(ctx, client, peerID, "server"); err != nil {
			log.Printf("[userbot %d] !server inline gagal: %v", msg.ID, err)
			_, sendErr := s.getSender(client).To(peerID).Text(ctx, "Status server tidak bisa dibuka. Pastikan inline mode bot aktif.")
			return sendErr
		}
		return nil
	}
	if name == "code" {
		return s.handleCodeCommand(ctx, client, peerID, msg, args)
	}
	if name == "gcast" || name == "broadcast" || name == "cancel" {
		if name == "cancel" {
			args = "cancel"
		}
		if err := s.broadcastCommand(ctx, client, peerID, msg, args); err != nil {
			log.Printf("[userbot %d] broadcast gagal: %v", msg.ID, err)
			return sendUserText(ctx, client, peerID, "Broadcast gagal: "+err.Error())
		}
		return nil
	}
	if name == "addbl" || name == "rmbl" {
		return s.blacklistCommand(ctx, client, peerID, msg, entities, args, map[string]string{"addbl": "add", "rmbl": "remove"}[name])
	}
	if name == "rmallbl" {
		return s.clearBlacklist(ctx, client, peerID)
	}
	if name == "lsbl" {
		return s.listBlacklist(ctx, client, peerID)
	}
	if name == "addsudo" || name == "rmsudo" {
		return s.sudoCommand(ctx, client, peerID, msg, args, name == "addsudo")
	}
	if name == "save" {
		return s.saveNoteCommand(ctx, client, peerID, msg, args)
	}
	if name == "log" {
		return s.handleLogCommand(ctx, client, peerID, args)
	}
	if name == "statuslog" {
		return s.handleStatusLogCommand(ctx, client, peerID)
	}
	if name == "get" {
		fields := strings.Fields(args)
		if len(fields) == 0 {
			return sendUserText(ctx, client, peerID, "Gunakan: !get <nama>")
		}
		return s.showNote(ctx, client, peerID, fields[0], false)
	}
	if name == "cat" {
		noFm := false
		catName := ""
		fields := strings.Fields(args)
		if len(fields) > 0 {
			catName = fields[0]
			for _, f := range fields[1:] {
				if strings.ToLower(f) == "-nofm" {
					noFm = true
				}
			}
		}
		if catName == "" {
			return sendUserText(ctx, client, peerID, "Gunakan: !cat <nama> [-nofm]")
		}
		return s.showNote(ctx, client, peerID, catName, noFm)
	}
	if name == "lsnote" || name == "notes" {
		return s.listNotesCommand(ctx, client, peerID)
	}
	if name == "delnote" {
		return s.deleteNoteCommand(ctx, client, peerID, args)
	}
	if name == "clearnote" {
		return s.clearNotesCommand(ctx, client, peerID)
	}
	if name == "nofm" {
		return s.nofmCommand(ctx, client, peerID, msg, args)
	}
	if name == "exportnote" {
		return s.exportNotesCommand(ctx, client, peerID)
	}
	if name == "importnote" {
		return s.importNotesCommand(ctx, client, peerID, msg)
	}
	if name == "eval" || name == "sh" {
		if name == "eval" {
			return s.evaluatorCommand(ctx, client, peerID, msg, entities, prefix, args)
		}
		return s.shellCommand(ctx, client, peerID, prefix, args)
	}
	if name == "simulasi" {
		return s.handleSimulasi(ctx, client, peerID, msg, args)
	}
	if name != "ping" && name != "p" {
		return nil
	}
	started := time.Now()
	text := "Ping gagal. Periksa koneksi dan status sesi userbot."
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx); err == nil {
		latency := float64(time.Since(started).Microseconds()) / 1000
		if name == "p" {
			text = fmt.Sprintf("Pong! %.3f ms", latency)
		} else {
			text = fmt.Sprintf("<code>%.3f ms</code>", latency)
		}
	}
	sender := s.getSender(client)
	if strings.HasPrefix(text, "<code>") {
		text = strings.TrimSuffix(strings.TrimPrefix(text, "<code>"), "</code>")
		_, err = sender.To(peerID).StyledText(pingCtx, styling.Code(text))
	} else {
		_, err = sender.To(peerID).Text(pingCtx, text)
	}
	return err
}
func (s *Service) handleSimulasi(ctx context.Context, client *telegram.Client, peerID tg.InputPeerClass, msg *tg.Message, args string) error {
	// Hanya OWNER yang boleh menjalankan simulasi
	var callerID int64
	if pu, ok := msg.FromID.(*tg.PeerUser); ok {
		callerID = pu.UserID
	}
	if callerID == 0 || callerID != s.ownerID {
		_, err := s.getSender(client).To(peerID).Text(ctx, "<b>❌ Akses Ditolak</b>\nPerintah /simulasi hanya bisa digunakan oleh OWNER.")
		return err
	}

	fields := strings.Fields(args)
	if len(fields) >= 1 && fields[0] == "stop" {
		s.simMu.Lock()
		if !s.simulasiActive {
			s.simMu.Unlock()
			_, err := s.getSender(client).To(peerID).Text(ctx, "<b>ℹ️ Simulasi tidak sedang aktif.</b>")
			return err
		}
		// Stop CPU goroutines
		if s.simulasiCancel != nil {
			s.simulasiCancel()
			s.simulasiCancel = nil
		}
		// Buang alokasi RAM
		s.simulasiMem = nil
		s.simulasiActive = false
		s.simulasiStres = 0
		s.simulasiIdle = 0
		s.simulasiAktif = 0
		s.simulasiUpdatedAt = time.Now()
		s.simMu.Unlock()

		// Sync ke Redis untuk telemetry
		s.publishSimulasiState(ctx, false, 0, 0, 0)

		_, err := s.getSender(client).To(peerID).Text(ctx, "<b>🛑 Simulasi dihentikan.</b> Semua resource simulasi telah dibuang.")
		return err
	}

	var stres, idle, aktif int
	if len(fields) >= 1 {
		fmt.Sscanf(fields[0], "%d", &stres)
	}
	if len(fields) >= 2 {
		fmt.Sscanf(fields[1], "%d", &idle)
	}
	if len(fields) >= 3 {
		fmt.Sscanf(fields[2], "%d", &aktif)
	}
	total := stres + idle + aktif

	if total == 0 {
		_, err := s.getSender(client).To(peerID).Text(ctx, "<b>ℹ️ Penggunaan:</b>\n<code>/simulasi <stres> <idle> <aktif></code>\nContoh: <code>/simulasi 200 300 500</code>\nUntuk menghentikan: <code>/simulasi stop</code>")
		return err
	}

	if total > 2000 {
		_, err := s.getSender(client).To(peerID).Text(ctx, "<b>⚠️ Total terlalu besar.</b> Maksimal 2000 sesi simulasi untuk mencegah OOM.")
		return err
	}

	// Hentikan simulasi lama jika ada
	s.simMu.Lock()
	if s.simulasiActive && s.simulasiCancel != nil {
		s.simulasiCancel()
		s.simulasiCancel = nil
	}
	s.simulasiMem = nil

	// Allokasi RAM nyata: blok memori yang dipertahankan agar tidak di-GC
	memBlocks := make([][]byte, 0, total)
	for i := 0; i < stres; i++ {
		// Sesi stres: alokasi 2 MB per sesi, berulang-ulk
		block := make([]byte, 2*1024*1024)
		for j := range block {
			block[j] = byte(j)
		}
		memBlocks = append(memBlocks, block)
	}
	for i := 0; i < idle; i++ {
		// Sesi idle: alokasi 1 MB per sesi
		memBlocks = append(memBlocks, make([]byte, 1*1024*1024))
	}
	for i := 0; i < aktif; i++ {
		// Sesi aktif: alokasi 3 MB per sesi
		memBlocks = append(memBlocks, make([]byte, 3*1024*1024))
	}

	s.simulasiMem = memBlocks
	s.simulasiActive = true
	s.simulasiStres = stres
	s.simulasiIdle = idle
	s.simulasiAktif = aktif
	s.simulasiUpdatedAt = time.Now()

	// Spawn goroutine CPU yang benar-benar bekerja (bukan tidur)
	simCtx, simCancel := context.WithCancel(context.Background())
	s.simulasiCancel = simCancel
	var cpuCounter int64
	for i := 0; i < stres*4+idle*2+aktif*6+50; i++ {
		go func() {
			h := sha256.New()
			buf := make([]byte, 64)
			for {
				select {
				case <-simCtx.Done():
					return
				default:
					h.Write(buf)
					_ = hex.EncodeToString(h.Sum(nil))
					h.Reset()
					atomic.AddInt64(&cpuCounter, 1)
					// Beri jeda singkat agar tidak sepenuhnya mendominasi CPU satu core
					// Tapi masih membuat beban CPU nyata yang terdeteksi
					time.Sleep(time.Microsecond * 100)
				}
			}
		}()
	}
	s.simMu.Unlock()

	// Sync ke Redis untuk telemetry live
	s.publishSimulasiState(ctx, true, stres, idle, aktif)

	ramStress := stres * 2
	ramIdle := idle * 1
	ramAktif := aktif * 3
	ramTotal := ramStress + ramIdle + ramAktif

	goroStress := stres * 4
	goroIdle := idle * 2
	goroAktif := aktif * 6
	goroTotal := goroStress + goroIdle + goroAktif + 50

	report := fmt.Sprintf(`<h1>🧪 Simulasi MTProto GotdBot (REAL LOAD)</h1>
<blockquote expandable>Simulasi sedang berjalan dengan alokasi RAM dan CPU nyata. Tidak ada koneksi Telegram aktual.</blockquote>
<table border="1">
  <tr><th>Item</th><th>Jumlah</th><th>Estimasi RAM</th></tr>
  <tr><td>Sesi Stres</td><td><code>%d</code></td><td><code>%d MB</code></td></tr>
  <tr><td>Sesi Idle</td><td><code>%d</code></td><td><code>%d MB</code></td></tr>
  <tr><td>Sesi Aktif</td><td><code>%d</code></td><td><code>%d MB</code></td></tr>
  <tr><td><b>Total</b></td><td><b><code>%d</code></b></td><td><b><code>~%d MB</code></b></td></tr>
</table>
<table border="1">
  <tr><th>Runtime Simulasi</th><th>Estimasi</th></tr>
  <tr><td>Goroutine CPU</td><td><code>~%d</code></td></tr>
  <tr><td>TCP Koneksi Virtual</td><td><code>~%d</code></td></tr>
  <tr><td>AuthKey Virtual</td><td><code>~%d</code></td></tr>
</table>
<hr/>
<p>GotdBot Simulasi Mode • Ketik <code>/simulasi stop</code> untuk menghentikan</p>`,
		stres, ramStress,
		idle, ramIdle,
		aktif, ramAktif,
		total, ramTotal,
		goroTotal,
		total, // TCP koneksi ≈ total sesi
		total, // AuthKey ≈ total sesi
	)

	_, err := s.getSender(client).To(peerID).Text(ctx, report)
	return err
}

// publishSimulasiState menyimpan state simulasi ke Redis agar gateway telemetry bisa membacanya.
func (s *Service) publishSimulasiState(ctx context.Context, active bool, stres, idle, aktif int) {
	if s.redis == nil {
		return
	}
	s.simMu.Lock()
	state := "inactive"
	if active {
		state = "active"
	}
	s.simMu.Unlock()

	payload := map[string]any{
		"active":    active,
		"state":     state,
		"stres":     stres,
		"idle":      idle,
		"aktif":     aktif,
		"updated_at": s.simulasiUpdatedAt.Unix(),
	}
	jsonData, _ := json.Marshal(payload)
	_ = s.redis.Client.Set(ctx, "simulasi:state", jsonData, 0).Err()
}

func (s *Service) cacheEntities(entities tg.Entities) {
	for _, u := range entities.Users {
		if u != nil && u.ID != 0 && u.AccessHash != 0 {
			s.userAccessHashes.Store(u.ID, u.AccessHash)
		}
	}
	for _, ch := range entities.Channels {
		if ch != nil && ch.ID != 0 && ch.AccessHash != 0 {
			s.channelAccessHashes.Store(ch.ID, ch.AccessHash)
		}
	}
}

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
		h.s.mu.Lock()
		ownerID := h.s.owners[h.chatID]
		h.s.mu.Unlock()

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
		h.s.mu.Lock()
		ownerID := h.s.owners[h.chatID]
		h.s.mu.Unlock()

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
