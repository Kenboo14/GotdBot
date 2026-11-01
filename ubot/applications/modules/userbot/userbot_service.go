package userbot

import (
	"assterisk/ubot/applications/databases"
	"assterisk/ubot/applications/models"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Service struct {
	root             context.Context
	db               *mongo.Database
	redis            *databases.Redis
	apiID            int
	apiHash          string
	key              []byte
	botToken         string
	inlineMu         sync.Mutex
	botUsernameCache string
	inlineBots       map[*telegram.Client]*tg.InputUser
	mu               sync.Mutex
	logins           map[int64]*login
	active           map[int64]*telegram.Client
	activeCancels    map[int64]context.CancelFunc // cancel func untuk idle disconnect
	lastActive       map[int64]time.Time          // chatID -> waktu aktivitas terakhir
	owners           map[int64]int64              // chatID -> ownerID
	clientOwners     map[*telegram.Client]int64   // client -> ownerID
	prefixes         map[int64]string
	broadcasts       map[int64]context.CancelFunc
	sudoMu           sync.RWMutex
	sudoCache        map[string]bool // "ownerID:userID" -> bool
	broadcastSem     chan struct{}   // Semaphore untuk membatasi concurrent gcast
}
type login struct {
	cancel   context.CancelFunc
	otp      chan string
	password chan string
}
type record struct {
	ChatID    int64     `bson:"_id"`
	OwnerID   int64     `bson:"owner_id,omitempty"`
	AccountID int64     `bson:"account_id"`
	Blob      string    `bson:"session"`
	Prefix    string    `bson:"prefix,omitempty"`
	PrefixSet bool      `bson:"prefix_configured"`
	UpdatedAt time.Time `bson:"updated_at"`
}

func NewService(root context.Context, db *mongo.Database, redis *databases.Redis, apiID int, apiHash string, key []byte, botToken string) *Service {
	return &Service{
		root:             root,
		db:               db,
		redis:            redis,
		apiID:            apiID,
		apiHash:          apiHash,
		key:              key,
		botToken:         botToken,
		logins:           make(map[int64]*login),
		active:           make(map[int64]*telegram.Client),
		activeCancels:    make(map[int64]context.CancelFunc),
		lastActive:       make(map[int64]time.Time),
		owners:           make(map[int64]int64),
		clientOwners:     make(map[*telegram.Client]int64),
		prefixes:         make(map[int64]string),
		broadcasts:       make(map[int64]context.CancelFunc),
		inlineBots:       make(map[*telegram.Client]*tg.InputUser),
		sudoCache:        make(map[string]bool),
		broadcastSem:     make(chan struct{}, 5), // maksimal 5 gcast bersamaan untuk hemat CPU & RAM
	}
}

func (s *Service) HandleCommand(ctx context.Context, msg redis.XMessage) {
	payload, ok := msg.Values["payload"].(string)
	if !ok {
		return
	}
	var job models.PToService
	if json.Unmarshal([]byte(payload), &job) != nil || job.ChatID == 0 {
		return
	}
	if job.Type == "broadcast_cancel" {
		ownerID := job.OwnerID
		if ownerID == 0 {
			ownerID = job.UserID
		}
		s.mu.Lock()
		cancel := s.broadcasts[ownerID]
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return
	}
	s.mu.Lock()
	client := s.active[job.UserID]
	targetID := job.UserID
	if client == nil {
		client = s.active[job.ChatID]
		targetID = job.ChatID
	}
	if client == nil {
		// On-demand wake up: jika sesi ada di database tapi sedang idle (dimatikan oleh reaper)
		s.mu.Unlock()
		s.activate(job.ChatID, job.UserID)
		// Tunggu sejenak hingga client siap
		for i := 0; i < 15; i++ {
			time.Sleep(200 * time.Millisecond)
			s.mu.Lock()
			client = s.active[job.ChatID]
			if client == nil {
				client = s.active[job.UserID]
			}
			if client != nil {
				targetID = job.ChatID
				break
			}
			s.mu.Unlock()
		}
	} else {
		s.lastActive[targetID] = time.Now()
		s.mu.Unlock()
	}
	if client == nil {
		s.reply(ctx, job.ChatID, "Ping gagal. Tidak ada sesi userbot aktif.", "failed")
		return
	}
	started := time.Now()
	if err := client.Ping(ctx); err != nil {
		s.reply(ctx, job.ChatID, "Ping gagal. Periksa koneksi dan status sesi userbot.", "failed")
		return
	}
	s.reply(ctx, job.ChatID, fmt.Sprintf("<code>%.3f ms</code>", float64(time.Since(started).Microseconds())/1000), "ok")
}

func (s *Service) Restore(ctx context.Context) {
	cur, err := s.db.Collection("userbot_sessions").Find(ctx, bson.M{"account_id": bson.M{"$gt": 0}})
	if err != nil {
		return
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		var row record
		if cur.Decode(&row) == nil {
			prefix := row.Prefix
			if !row.PrefixSet {
				prefix = "!"
			}
			s.mu.Lock()
			s.prefixes[row.ChatID] = prefix
			s.owners[row.ChatID] = row.AccountID
			s.mu.Unlock()
			s.activate(row.ChatID, row.AccountID)
		}
	}
	// Jalankan reaper di background untuk mematikan sesi yang idle > 30 menit
	go s.startIdleReaper(ctx)
}

func (s *Service) startIdleReaper(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reapIdleSessions()
		}
	}
}

func (s *Service) reapIdleSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for chatID, cancel := range s.activeCancels {
		last, ok := s.lastActive[chatID]
		// Jika tidak ada aktivitas lebih dari 30 menit dan sedang tidak menjalankan broadcast
		if ok && now.Sub(last) > 30*time.Minute {
			ownerID := s.owners[chatID]
			if s.broadcasts[ownerID] == nil && cancel != nil {
				log.Printf("[IdleReaper] Menutup sesi idle chatID=%d untuk hemat RAM", chatID)
				cancel()
				delete(s.activeCancels, chatID)
			}
		}
	}
}

func (s *Service) Handle(ctx context.Context, msg redis.XMessage) {
	payload, ok := msg.Values["payload"].(string)
	if !ok {
		return
	}
	var job models.PToService
	if json.Unmarshal([]byte(payload), &job) != nil || job.ChatID == 0 {
		return
	}
	s.mu.Lock()
	current := s.logins[job.ChatID]
	s.mu.Unlock()
	switch job.Type {
	case "auth_check":
		installed, err := s.hasInstalledSession(ctx, job.ChatID, job.UserID)
		if err != nil {
			s.reply(ctx, job.ChatID, "Tidak dapat memeriksa sesi userbot. Coba lagi.", "failed")
		} else if installed {
			s.reply(ctx, job.ChatID, "Userbot sudah terpasang untuk akun ini. Satu pengguna hanya dapat memiliki satu sesi aktif.", "failed")
		} else {
			s.reply(ctx, job.ChatID, "Masukkan nomor Telegram dalam format internasional, contoh: +6281234567890\n\nKetik /cancel untuk membatalkan.", "waiting_phone")
		}
	case "auth_phone":
		if current != nil {
			s.reply(ctx, job.ChatID, "Pemasangan masih berjalan. Ketik /cancel bila ingin memulai ulang.", "waiting_otp")
			return
		}
		if installed, err := s.hasInstalledSession(ctx, job.ChatID, job.UserID); err != nil {
			s.reply(ctx, job.ChatID, "Tidak dapat memeriksa sesi userbot. Coba lagi.", "failed")
		} else if installed {
			s.reply(ctx, job.ChatID, "Userbot sudah terpasang untuk akun ini. Satu pengguna hanya dapat memiliki satu sesi aktif.", "failed")
		} else {
			s.start(ctx, job.ChatID, job.UserID, strings.TrimSpace(job.Text))
		}
	case "auth_otp":
		if current == nil {
			s.reply(ctx, job.ChatID, "Sesi pemasangan sudah kedaluwarsa. Kirim /create lagi.", "failed")
			return
		}
		sendSecret(current.otp, job.Text)
	case "auth_password":
		if current == nil {
			s.reply(ctx, job.ChatID, "Sesi pemasangan sudah kedaluwarsa. Kirim /create lagi.", "failed")
			return
		}
		sendSecret(current.password, job.Text)
	case "auth_cancel":
		if current != nil {
			current.cancel()
		}
	}
}

func (s *Service) start(parent context.Context, chatID, ownerID int64, phone string) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	l := &login{cancel: cancel, otp: make(chan string, 1), password: make(chan string, 1)}
	s.mu.Lock()
	s.logins[chatID] = l
	s.mu.Unlock()
	go func() {
		defer cancel()
		defer s.remove(chatID)
		if err := s.authorize(ctx, chatID, ownerID, phone, l); err != nil && !errors.Is(err, context.Canceled) {
			s.reply(parent, chatID, "Pemasangan gagal: "+safeError(err), "failed")
		}
	}()
}

func (s *Service) authorize(ctx context.Context, chatID, ownerID int64, phone string, l *login) error {
	store := &mongoStorage{collection: s.db.Collection("userbot_sessions"), chatID: chatID, ownerID: ownerID, key: s.key}
	client := telegram.NewClient(s.apiID, s.apiHash, telegram.Options{SessionStorage: store, NoUpdates: true})
	return client.Run(ctx, func(ctx context.Context) error {
		sent, err := client.Auth().SendCode(ctx, phone, auth.SendCodeOptions{})
		if err != nil {
			return err
		}
		code, ok := sent.(*tg.AuthSentCode)
		if !ok {
			return fmt.Errorf("Telegram mengembalikan jenis OTP yang tidak didukung")
		}
		s.reply(ctx, chatID, "OTP telah dikirim. Balas dengan kode 4-10 digit; spasi boleh digunakan.", "waiting_otp")
		otp, err := receiveSecret(ctx, l.otp)
		if err != nil {
			return err
		}
		_, err = client.Auth().SignIn(ctx, phone, strings.ReplaceAll(otp, " ", ""), code.PhoneCodeHash)
		if errors.Is(err, auth.ErrPasswordAuthNeeded) {
			s.reply(ctx, chatID, "Akun memakai Verifikasi 2 Langkah. Kirim password 2FA Anda.", "waiting_password")
			password, err := receiveSecret(ctx, l.password)
			if err != nil {
				return err
			}
			if _, err = client.Auth().Password(ctx, password); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		self, err := client.Self(ctx)
		if err != nil {
			return err
		}
		if err := store.setAccount(ctx, self.ID); err != nil {
			return err
		}
		s.mu.Lock()
		s.prefixes[chatID] = "!"
		s.owners[chatID] = self.ID
		s.mu.Unlock()
		if err := store.setPrefix(ctx, "!", true); err != nil {
			return err
		}
		s.reply(ctx, chatID, fmt.Sprintf("Userbot berhasil dipasang untuk akun %d. Sesi disimpan terenkripsi.", self.ID), "installed")
		s.activate(chatID, self.ID)
		return nil
	})
}

func (s *Service) reply(ctx context.Context, chatID int64, text, state string) {
	_ = s.redis.To(ctx, "from:ubot", &models.PFromService{ChatID: chatID, Text: text, State: state})
}

func (s *Service) hasInstalledSession(ctx context.Context, chatID, ownerID int64) (bool, error) {
	filters := []bson.M{{"_id": chatID}}
	if ownerID > 0 {
		filters = append(filters, bson.M{"owner_id": ownerID})
	}
	var row record
	err := s.db.Collection("userbot_sessions").FindOne(ctx, bson.M{
		"$or":        filters,
		"account_id": bson.M{"$gt": 0},
	}).Decode(&row)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return row.AccountID > 0, nil
}

func (s *Service) remove(chatID int64) { s.mu.Lock(); delete(s.logins, chatID); s.mu.Unlock() }
func (s *Service) activate(chatID int64, knownOwnerID ...int64) {
	s.mu.Lock()
	if c, ok := s.active[chatID]; ok && c != nil {
		s.lastActive[chatID] = time.Now()
		s.mu.Unlock()
		return
	}
	var ownerID int64
	if len(knownOwnerID) > 0 {
		ownerID = knownOwnerID[0]
	}
	if ownerID == 0 {
		ownerID = s.owners[chatID]
	}
	// Reserve the key before starting the network client.
	s.active[chatID] = nil
	s.lastActive[chatID] = time.Now()
	clientCtx, clientCancel := context.WithCancel(s.root)
	s.activeCancels[chatID] = clientCancel
	s.mu.Unlock()
	go func() {
		var client *telegram.Client
		defer func() {
			s.mu.Lock()
			delete(s.active, chatID)
			delete(s.activeCancels, chatID)
			delete(s.lastActive, chatID)
			if client != nil {
				delete(s.clientOwners, client)
			}
			s.mu.Unlock()
			s.inlineMu.Lock()
			if client != nil {
				delete(s.inlineBots, client)
			}
			s.inlineMu.Unlock()
		}()
		store := &mongoStorage{collection: s.db.Collection("userbot_sessions"), chatID: chatID, key: s.key}
		dispatcher := tg.NewUpdateDispatcher()
		dispatcher.OnNewMessage(func(ctx context.Context, entities tg.Entities, update *tg.UpdateNewMessage) error {
			s.mu.Lock()
			s.lastActive[chatID] = time.Now()
			s.mu.Unlock()
			return s.handleUserbotMessage(ctx, chatID, client, entities, update)
		})
		dispatcher.OnNewChannelMessage(func(ctx context.Context, entities tg.Entities, update *tg.UpdateNewChannelMessage) error {
			// Telegram represents supergroup/channel messages with a separate
			// update constructor. Reuse the same outgoing-command dispatcher.
			s.mu.Lock()
			s.lastActive[chatID] = time.Now()
			s.mu.Unlock()
			return s.handleUserbotMessage(ctx, chatID, client, entities, &tg.UpdateNewMessage{Message: update.Message})
		})
		client = telegram.NewClient(s.apiID, s.apiHash, telegram.Options{SessionStorage: store, UpdateHandler: dispatcher})
		s.mu.Lock()
		s.active[chatID] = client
		if ownerID > 0 {
			s.owners[chatID] = ownerID
			s.clientOwners[client] = ownerID
		}
		s.mu.Unlock()
		_ = client.Run(clientCtx, func(ctx context.Context) error {
			self, err := client.Self(ctx)
			if err != nil {
				return err
			}
			s.mu.Lock()
			s.owners[chatID] = self.ID
			s.clientOwners[client] = self.ID
			s.mu.Unlock()
			<-ctx.Done()
			return ctx.Err()
		})
	}()
}

func (s *Service) handleUserbotMessage(ctx context.Context, chatID int64, client *telegram.Client, entities tg.Entities, update *tg.UpdateNewMessage) error {
	msg, ok := update.Message.(*tg.Message)
	if !ok {
		return nil
	}
	s.mu.Lock()
	prefix, ok := s.prefixes[chatID]
	s.mu.Unlock()
	if !ok {
		prefix = "!"
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
			_, sendErr := message.NewSender(tg.NewClient(client)).To(peerID).Text(ctx, "Help gagal. Periksa koneksi bot inline dan status sesi userbot.")
			return sendErr
		}
		return nil
	}
	if name == "server" {
		if err := s.sendInlineQuery(ctx, client, peerID, "server"); err != nil {
			log.Printf("[userbot %d] !server inline gagal: %v", msg.ID, err)
			_, sendErr := message.NewSender(tg.NewClient(client)).To(peerID).Text(ctx, "Status server tidak bisa dibuka. Pastikan inline mode bot aktif.")
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
	if name == "eval" || name == "sh" {
		if name == "eval" {
			return s.evaluatorCommand(ctx, client, peerID, msg, entities, prefix, args)
		}
		return s.shellCommand(ctx, client, peerID, prefix, args)
	}
	if name != "ping" && name != "p" {
		return nil
	}
	started := time.Now()
	text := "Ping gagal. Periksa koneksi dan status sesi userbot."
	if err := client.Ping(ctx); err == nil {
		latency := float64(time.Since(started).Microseconds()) / 1000
		if name == "p" {
			text = fmt.Sprintf("Pong! %.3f ms", latency)
		} else {
			text = fmt.Sprintf("<code>%.3f ms</code>", latency)
		}
	}
	if strings.HasPrefix(text, "<code>") {
		text = strings.TrimSuffix(strings.TrimPrefix(text, "<code>"), "</code>")
		_, err = message.NewSender(tg.NewClient(client)).To(peerID).StyledText(ctx, styling.Code(text))
	} else {
		_, err = message.NewSender(tg.NewClient(client)).To(peerID).Text(ctx, text)
	}
	return err
}

func sendSecret(ch chan string, value string) {
	select {
	case ch <- value:
	default:
	}
}
func receiveSecret(ctx context.Context, ch <-chan string) (string, error) {
	select {
	case value := <-ch:
		return value, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
func safeError(err error) string {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "phone_code_invalid"):
		return "OTP tidak valid"
	case strings.Contains(text, "phone_code_expired"):
		return "OTP sudah kedaluwarsa"
	case strings.Contains(text, "phone_number_invalid"):
		return "nomor telepon tidak valid"
	case strings.Contains(text, "phone_number_banned"):
		return "nomor telepon diblokir Telegram"
	case strings.Contains(text, "flood"):
		return "terlalu banyak percobaan; tunggu sebelum mencoba lagi"
	default:
		return "Telegram menolak permintaan. Periksa data dan coba lagi."
	}
}

type mongoStorage struct {
	collection *mongo.Collection
	chatID     int64
	ownerID    int64
	key        []byte
}

func (m *mongoStorage) LoadSession(ctx context.Context) ([]byte, error) {
	var row record
	err := m.collection.FindOne(ctx, bson.M{"_id": m.chatID}).Decode(&row)
	if err == mongo.ErrNoDocuments {
		return nil, session.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return decrypt(m.key, row.Blob)
}
func (m *mongoStorage) StoreSession(ctx context.Context, data []byte) error {
	blob, err := encrypt(m.key, data)
	if err != nil {
		return err
	}
	_, err = m.collection.UpdateByID(ctx, m.chatID, bson.M{"$set": bson.M{"owner_id": m.ownerID, "session": blob, "updated_at": time.Now().UTC()}}, options.Update().SetUpsert(true))
	return err
}
func (m *mongoStorage) setAccount(ctx context.Context, id int64) error {
	_, err := m.collection.UpdateByID(ctx, m.chatID, bson.M{"$set": bson.M{"account_id": id, "updated_at": time.Now().UTC()}})
	return err
}

func (m *mongoStorage) setPrefix(ctx context.Context, prefix string, configured bool) error {
	_, err := m.collection.UpdateByID(ctx, m.chatID, bson.M{"$set": bson.M{"prefix": prefix, "prefix_configured": configured, "updated_at": time.Now().UTC()}})
	return err
}
func encrypt(key, data []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(append(nonce, gcm.Seal(nil, nonce, data, nil)...)), nil
}
func decrypt(key []byte, input string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("session terenkripsi tidak valid")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
}
