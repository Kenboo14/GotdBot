package userbot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf16"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/tg"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	telegramServiceUserID int64 = 777000
	tagAllMentionLimit          = 5
	mentionBurstWindow          = 15 * time.Second
	mentionBurstLimit           = 4
	autoReplyDupWindow          = 15 * time.Minute
	autoReplyMinRunes           = 15
	botCacheTTL                 = time.Hour
)

type botCacheEntry struct {
	isBot     bool
	expiresAt time.Time
}

type mentionBurstKey struct {
	ownerID  int64
	chatID   int64
	senderID int64
}

type autoReplyKey struct {
	ownerID  int64
	senderID int64
	text     string
}

type autoReplyEntry struct {
	lastSeen time.Time
}

func extractLogSenderUserID(msg *tg.Message) int64 {
	if msg == nil {
		return 0
	}
	if fromUser, ok := msg.FromID.(*tg.PeerUser); ok && fromUser.UserID > 0 {
		return fromUser.UserID
	}
	if peerUser, ok := msg.PeerID.(*tg.PeerUser); ok && peerUser.UserID > 0 && !msg.Out {
		return peerUser.UserID
	}
	return 0
}

func isBotUsername(username string) bool {
	u := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))
	return u != "" && strings.HasSuffix(u, "bot")
}

// isKnownBotEntity memeriksa status bot dari entities.Users atau cache memori tanpa RPC.
// Mengembalikan (known, isBot). Jika known == false, pemanggil dapat fallback ke UsersGetUsers.
func (s *Service) isKnownBotEntity(entities tg.Entities, userID int64) (bool, bool) {
	if userID == 0 {
		return true, false
	}
	if userID == telegramServiceUserID {
		return true, true
	}
	if entities.Users != nil {
		if u := entities.Users[userID]; u != nil {
			if u.Bot || isBotUsername(u.Username) {
				if s != nil {
					s.storeBotCache(userID, true)
				}
				return true, true
			}
			if !u.Min {
				if s != nil {
					s.storeBotCache(userID, false)
				}
				return true, false
			}
		}
	}
	if s != nil {
		if val, ok := s.botUserCache.Load(userID); ok {
			if entry, ok := val.(botCacheEntry); ok && time.Now().Before(entry.expiresAt) {
				return true, entry.isBot
			}
		}
	}
	return false, false
}

func (s *Service) storeBotCache(userID int64, isBot bool) {
	if s == nil || userID == 0 {
		return
	}
	s.botUserCache.Store(userID, botCacheEntry{
		isBot:     isBot,
		expiresAt: time.Now().Add(botCacheTTL),
	})
}

// isUserBotMTProto memeriksa apakah userID adalah akun bot (entities -> cache -> fallback 1x UsersGetUsers).
func (s *Service) isUserBotMTProto(ctx context.Context, client *gobot.Client, entities tg.Entities, userID int64) bool {
	if known, isBot := s.isKnownBotEntity(entities, userID); known {
		return isBot
	}
	if client == nil || userID <= 0 {
		return false
	}
	var accessHash int64
	if entities.Users != nil {
		if u := entities.Users[userID]; u != nil {
			accessHash = u.AccessHash
		}
	}
	if accessHash == 0 {
		if h, ok := s.peerCache().UserHash(userID); ok {
			accessHash = h
		}
	}
	users, err := client.API().UsersGetUsers(ctx, []tg.InputUserClass{
		&tg.InputUser{UserID: userID, AccessHash: accessHash},
	})
	if err != nil {
		return false
	}
	s.peerCache().ApplyUsersChats(users, nil)
	for _, item := range users {
		if u, ok := item.(*tg.User); ok && u.ID == userID {
			isBot := u.Bot || isBotUsername(u.Username)
			s.storeBotCache(userID, isBot)
			return isBot
		}
	}
	return false
}

func extractUTF16EntityText(text string, offset, length int) string {
	if offset < 0 || length <= 0 || text == "" {
		return ""
	}
	units := utf16.Encode([]rune(text))
	if offset+length > len(units) {
		return ""
	}
	return string(utf16.Decode(units[offset : offset+length]))
}

// countUniqueMentionTargets menghitung jumlah target mention unik dalam 1 pesan grup/channel
// untuk mendeteksi pesan Userbot Tag-All / mass-mention.
func countUniqueMentionTargets(msg *tg.Message) int {
	if msg == nil || len(msg.Entities) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(msg.Entities))
	for _, ent := range msg.Entities {
		switch e := ent.(type) {
		case *tg.MessageEntityMentionName:
			if e.UserID > 0 {
				seen[fmt.Sprintf("id:%d", e.UserID)] = struct{}{}
			}
		case *tg.MessageEntityMention:
			raw := strings.ToLower(strings.TrimSpace(extractUTF16EntityText(msg.Message, e.Offset, e.Length)))
			if raw != "" {
				seen["u:"+raw] = struct{}{}
			} else {
				seen[fmt.Sprintf("pos:%d:%d", e.Offset, e.Length)] = struct{}{}
			}
		case *tg.MessageEntityTextURL:
			low := strings.ToLower(e.URL)
			if strings.HasPrefix(low, "tg://user?id=") {
				seen["url:"+low] = struct{}{}
			}
		}
	}
	return len(seen)
}

func isMassMentionOrTagAll(msg *tg.Message) bool {
	return countUniqueMentionTargets(msg) >= tagAllMentionLimit
}

func (s *Service) recordAndCheckMentionBurst(ownerID, chatID, senderID int64, mentionCount int, now time.Time) bool {
	if s == nil || mentionCount <= 0 || senderID <= 0 || chatID == 0 {
		return false
	}
	key := mentionBurstKey{ownerID: ownerID, chatID: chatID, senderID: senderID}
	cutoff := now.Add(-mentionBurstWindow)

	s.logFilterMu.Lock()
	defer s.logFilterMu.Unlock()
	if s.mentionBursts == nil {
		s.mentionBursts = make(map[mentionBurstKey][]time.Time)
	}
	if len(s.mentionBursts) > 512 {
		for k, times := range s.mentionBursts {
			if len(times) == 0 || times[len(times)-1].Before(cutoff) {
				delete(s.mentionBursts, k)
			}
		}
	}
	prev := s.mentionBursts[key]
	recent := prev[:0]
	for _, ts := range prev {
		if ts.After(cutoff) {
			recent = append(recent, ts)
		}
	}
	recent = append(recent, now)
	s.mentionBursts[key] = recent
	return len(recent) >= mentionBurstLimit
}

func normalizeAutoReplyText(raw string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(raw)))
	return strings.Join(fields, " ")
}

// isDuplicateAutoReply mendeteksi pesan auto-reply / AFK berulang yang identik
// dari pengirim yang sama dalam rentang 15 menit.
func (s *Service) isDuplicateAutoReply(ownerID, senderID int64, rawText string, now time.Time) bool {
	if s == nil || senderID <= 0 {
		return false
	}
	norm := normalizeAutoReplyText(rawText)
	if len([]rune(norm)) < autoReplyMinRunes {
		return false
	}
	key := autoReplyKey{ownerID: ownerID, senderID: senderID, text: norm}
	cutoff := now.Add(-autoReplyDupWindow)

	s.logFilterMu.Lock()
	defer s.logFilterMu.Unlock()
	if s.autoReplyHist == nil {
		s.autoReplyHist = make(map[autoReplyKey]autoReplyEntry)
	}
	if len(s.autoReplyHist) > 1024 {
		for k, entry := range s.autoReplyHist {
			if entry.lastSeen.Before(cutoff) {
				delete(s.autoReplyHist, k)
			}
		}
	}
	if prev, ok := s.autoReplyHist[key]; ok && prev.lastSeen.After(cutoff) {
		s.autoReplyHist[key] = autoReplyEntry{lastSeen: now}
		return true
	}
	s.autoReplyHist[key] = autoReplyEntry{lastSeen: now}
	return false
}

// ─── Log Block Management (!addblock, !rmblock, !lsblock, !rmallblock) ─────

func (s *Service) isUserLogBlocked(ctx context.Context, ownerID, userID int64) bool {
	if s == nil || ownerID == 0 || userID == 0 {
		return false
	}
	cacheKey := fmt.Sprintf("%d:%d", ownerID, userID)
	if val, ok := s.logBlockCache.Load(cacheKey); ok {
		return val.(bool)
	}
	blocked := false
	if s.sqlite != nil {
		if b, err := s.sqlite.IsLogBlocked(ctx, ownerID, userID); err == nil {
			blocked = b
		}
	}
	s.logBlockCache.Store(cacheKey, blocked)
	return blocked
}

func (s *Service) handleLogBlockCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, args string, add bool, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil || ownerID == 0 {
		return sendUserText(ctx, client, target, "Gagal mendapatkan ID pemilik userbot.", threadID)
	}
	userID, displayName, resolveErr := s.resolveTargetUserID(ctx, client, target, msg, entities, args)
	if resolveErr != nil {
		if errors.Is(resolveErr, errNoTargetUser) {
			cmd := "!addblock"
			if !add {
				cmd = "!rmblock"
			}
			return sendUserText(ctx, client, target, "Balas pesan pengguna atau gunakan: "+cmd+" <user_id|@username>", threadID)
		}
		return sendUserText(ctx, client, target, resolveErr.Error(), threadID)
	}
	if add && userID == ownerID {
		return sendUserText(ctx, client, target, "Tidak perlu memasukkan ID pemilik userbot ke list blocker log.", threadID)
	}

	cacheKey := fmt.Sprintf("%d:%d", ownerID, userID)
	targetLabel := fmt.Sprintf("%d", userID)
	if displayName != "" {
		targetLabel = fmt.Sprintf("%s (%d)", displayName, userID)
	}

	if add {
		if s.sqlite != nil {
			if dbErr := s.sqlite.AddLogBlock(ctx, ownerID, userID, displayName); dbErr != nil {
				return sendUserText(ctx, client, target, "Gagal menyimpan ke list blocker log: "+dbErr.Error(), threadID)
			}
		}
		s.logBlockCache.Store(cacheKey, true)
		if s.db != nil {
			go s.logSaveBlockMongo(ownerID, userID, displayName)
		}
		return sendUserText(ctx, client, target, "Ditambahkan ke list blocker Log Bot (pesannya tidak akan masuk log): "+targetLabel, threadID)
	}

	if s.sqlite != nil {
		if dbErr := s.sqlite.RemoveLogBlock(ctx, ownerID, userID); dbErr != nil {
			return sendUserText(ctx, client, target, "Gagal menghapus dari list blocker log: "+dbErr.Error(), threadID)
		}
	}
	s.logBlockCache.Store(cacheKey, false)
	if s.db != nil {
		go s.logDeleteBlockMongo(ownerID, userID)
	}
	return sendUserText(ctx, client, target, "Dihapus dari list blocker Log Bot (pesannya kembali dicatat di log): "+targetLabel, threadID)
}

func (s *Service) handleListLogBlockCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil || ownerID == 0 {
		return sendUserText(ctx, client, target, "Gagal mendapatkan ID pemilik userbot.", threadID)
	}
	if s.sqlite == nil {
		return sendUserText(ctx, client, target, "Database SQLite tidak tersedia.", threadID)
	}
	rows, err := s.sqlite.ListLogBlocks(ctx, ownerID)
	if err != nil {
		return sendUserText(ctx, client, target, "Gagal membaca list blocker log: "+err.Error(), threadID)
	}
	if len(rows) == 0 {
		return sendUserText(ctx, client, target, "List blocker Log Bot masih kosong.", threadID)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("List blocker Log Bot (%d user — pesan tidak masuk log):\n", len(rows)))
	for i, r := range rows {
		if r.Label != "" {
			sb.WriteString(fmt.Sprintf("%d. %s (%d)\n", i+1, r.Label, r.UserID))
		} else {
			sb.WriteString(fmt.Sprintf("%d. %d\n", i+1, r.UserID))
		}
	}
	return sendUserText(ctx, client, target, strings.TrimRight(sb.String(), "\n"), threadID)
}

func (s *Service) handleClearLogBlockCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, threadID int) error {
	ownerID, err := s.userbotOwner(ctx, client)
	if err != nil || ownerID == 0 {
		return sendUserText(ctx, client, target, "Gagal mendapatkan ID pemilik userbot.", threadID)
	}
	if s.sqlite != nil {
		if dbErr := s.sqlite.ClearLogBlocks(ctx, ownerID); dbErr != nil {
			return sendUserText(ctx, client, target, "Gagal membersihkan daftar blokir log: "+dbErr.Error(), threadID)
		}
	}
	prefix := fmt.Sprintf("%d:", ownerID)
	s.logBlockCache.Range(func(k, _ any) bool {
		if key, ok := k.(string); ok && strings.HasPrefix(key, prefix) {
			s.logBlockCache.Delete(key)
		}
		return true
	})
	if s.db != nil {
		go s.logClearBlocksMongo(ownerID)
	}
	return sendUserText(ctx, client, target, "Semua user di daftar blokir Log Bot telah dihapus.", threadID)
}

func (s *Service) logSaveBlockMongo(ownerID, userID int64, label string) {
	if s.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	col := s.db.Collection("userbot_log_blocks")
	filter := bson.M{"owner_id": ownerID, "user_id": userID}
	update := bson.M{
		"$set": bson.M{
			"owner_id":   ownerID,
			"user_id":    userID,
			"label":      label,
			"updated_at": time.Now(),
		},
	}
	_, _ = col.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
}

func (s *Service) logDeleteBlockMongo(ownerID, userID int64) {
	if s.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.db.Collection("userbot_log_blocks").DeleteOne(ctx, bson.M{"owner_id": ownerID, "user_id": userID})
}

func (s *Service) logClearBlocksMongo(ownerID int64) {
	if s.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.db.Collection("userbot_log_blocks").DeleteMany(ctx, bson.M{"owner_id": ownerID})
}

func (s *Service) logLoadBlocksFromMongo(ctx context.Context) {
	if s.db == nil || s.sqlite == nil {
		return
	}
	cur, err := s.db.Collection("userbot_log_blocks").Find(ctx, bson.M{})
	if err != nil {
		return
	}
	defer cur.Close(ctx)

	synced := 0
	for cur.Next(ctx) {
		var doc struct {
			OwnerID int64  `bson:"owner_id"`
			UserID  int64  `bson:"user_id"`
			Label   string `bson:"label"`
		}
		if err := cur.Decode(&doc); err != nil || doc.OwnerID == 0 || doc.UserID == 0 {
			continue
		}
		if s.sqlite.AddLogBlock(ctx, doc.OwnerID, doc.UserID, doc.Label) == nil {
			s.logBlockCache.Store(fmt.Sprintf("%d:%d", doc.OwnerID, doc.UserID), true)
			synced++
		}
	}
	if synced > 0 {
		log.Printf("📋 Sinkronisasi MongoDB → SQLite: %d daftar block log dimuat", synced)
	}
}
