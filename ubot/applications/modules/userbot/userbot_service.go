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
	"os"
	"strconv"
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

// idleTimeoutMinutes controls how long an inactive session stays alive before
// the reaper disconnects it. Override with IDLE_TIMEOUT_MINUTES env var.
var idleTimeoutMinutes = 30

// maxActiveSessions is the admission limit on simultaneously active/connecting MTProto clients.
// Override with MAX_ACTIVE_SESSIONS env var. When reached, new activations are rejected
// to protect existing active sessions from eviction (admission control policy).
var maxActiveSessions = 100

func init() {
	if v, err := strconv.Atoi(os.Getenv("IDLE_TIMEOUT_MINUTES")); err == nil && v > 0 {
		idleTimeoutMinutes = v
	}
	if v, err := strconv.Atoi(os.Getenv("MAX_ACTIVE_SESSIONS")); err == nil && v > 0 {
		maxActiveSessions = v
	}
}

type Service struct {
	root             context.Context
	sqlite           *databases.SQLiteClient
	db               *mongo.Database
	redis            *databases.Redis
	apiID            int
	apiHash          string
	key              []byte
	botToken         string
	logBotToken      string
	logBotID         int64
	logBotUsername   string
	startedLogBots      sync.Map // key: [2]int64{userbotID, logBotID} -> true
	logStatusCache      sync.Map // key: int64 (ownerID) -> bool
	ownerUsernames      sync.Map // key: int64 (ownerID) -> lowercase username without @
	userAccessHashes    sync.Map // key: int64 (userID) -> int64 (accessHash)
	channelAccessHashes sync.Map // key: int64 (channelID) -> int64 (accessHash)
	inlineMu         sync.RWMutex
	botUsernameCache string
	inlineBots       map[*telegram.Client]*tg.InputUser
	mu               sync.Mutex
	logins           map[int64]*login
	active           map[int64]*telegram.Client
	connecting       map[int64]uint64             // guard terhadap double activation & reservasi slot (chatID -> genID)
	activeCancels    map[int64]context.CancelFunc // cancel func untuk graceful shutdown
	accountCtxs      map[int64]context.Context    // chatID -> clientCtx (account lifecycle context)
	accountGens      map[int64]uint64             // chatID -> token kepemilikan generasi lifecycle
	nextGen          uint64                       // counter generasi unik untuk lifecycle
	lastActive       map[int64]time.Time          // chatID -> waktu aktivitas terakhir
	owners           map[int64]int64              // chatID -> ownerID
	clientOwners     map[*telegram.Client]int64   // client -> ownerID
	senderCache      map[*telegram.Client]*message.Sender // cached message.Sender per client
	prefixes         map[int64]string
	broadcasts       map[int64]*broadcastJob
	sudoMu           sync.RWMutex
	sudoCache        map[string]sudoCacheEntry // "ownerID:userID" -> sudoCacheEntry dengan TTL
	broadcastSem     chan struct{}             // Semaphore untuk membatasi concurrent gcast
}
type login struct {
	cancel    context.CancelFunc
	otp       chan string
	password  chan string
	createdAt time.Time
}
type broadcastJob struct {
	id     string
	cancel context.CancelFunc
}
type sudoCacheEntry struct {
	allowed   bool
	expiresAt time.Time
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

func NewService(root context.Context, sqlite *databases.SQLiteClient, db *mongo.Database, redis *databases.Redis, apiID int, apiHash string, key []byte, botToken, logBotToken string) *Service {
	return &Service{
		root:             root,
		sqlite:           sqlite,
		db:               db,
		redis:            redis,
		apiID:            apiID,
		apiHash:          apiHash,
		key:              key,
		botToken:         botToken,
		logBotToken:      logBotToken,
		logins:           make(map[int64]*login),
		active:           make(map[int64]*telegram.Client),
		connecting:       make(map[int64]uint64),
		activeCancels:    make(map[int64]context.CancelFunc),
		accountCtxs:      make(map[int64]context.Context),
		accountGens:      make(map[int64]uint64),
		lastActive:       make(map[int64]time.Time),
		owners:           make(map[int64]int64),
		clientOwners:     make(map[*telegram.Client]int64),
		senderCache:      make(map[*telegram.Client]*message.Sender),
		prefixes:         make(map[int64]string),
		broadcasts:       make(map[int64]*broadcastJob),
		inlineBots:       make(map[*telegram.Client]*tg.InputUser),
		sudoCache:        make(map[string]sudoCacheEntry),
		broadcastSem:     make(chan struct{}, 5), // maksimal 5 gcast bersamaan untuk hemat CPU & RAM
	}
}

// getSender returns a cached message.Sender for the given client, avoiding
// repeated allocation of the wrapper on every command invocation.
func (s *Service) getSender(client *telegram.Client) *message.Sender {
	s.mu.Lock()
	sender, ok := s.senderCache[client]
	if !ok {
		sender = message.NewSender(tg.NewClient(client))
		s.senderCache[client] = sender
	}
	s.mu.Unlock()
	return sender
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
		bJob := s.broadcasts[ownerID]
		s.mu.Unlock()
		if bJob != nil {
			bJob.cancel()
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
		for cid, oid := range s.owners {
			if oid == job.UserID || oid == job.ChatID {
				if c := s.active[cid]; c != nil {
					client = c
					targetID = cid
					break
				}
			}
		}
	}
	if client == nil {
		// On-demand wake up: jika sesi ada di database tapi belum aktif
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
			if client == nil {
				for cid, oid := range s.owners {
					if oid == job.UserID || oid == job.ChatID {
						if c := s.active[cid]; c != nil {
							client = c
							targetID = cid
							break
						}
					}
				}
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
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx); err != nil {
		s.reply(ctx, job.ChatID, "Ping gagal. Periksa koneksi dan status sesi userbot.", "failed")
		return
	}
	s.reply(ctx, job.ChatID, fmt.Sprintf("<code>%.3f ms</code>", float64(time.Since(started).Microseconds())/1000), "ok")
}

func (s *Service) Restore(ctx context.Context) {
	// 1. Sinkronisasi Sesi dari MongoDB Cloud Atlas ke SQLite lokal
	if s.db != nil && s.sqlite != nil {
		log.Println("🔄 Memeriksa sinkronisasi sesi dari MongoDB Cloud Atlas...")
		cur, err := s.db.Collection("userbot_sessions").Find(ctx, bson.M{
			"$or": []bson.M{
				{"session": bson.M{"$ne": ""}},
				{"account_id": bson.M{"$gt": 0}},
			},
		})
		if err == nil {
			defer cur.Close(ctx)
			synced := 0
			for cur.Next(ctx) {
				var row record
				if cur.Decode(&row) == nil && row.Blob != "" {
					accID := row.AccountID
					if accID == 0 {
						accID = row.OwnerID
					}
					if accID == 0 {
						accID = row.ChatID
					}
					if err := s.sqlite.SaveSession(ctx, row.ChatID, row.OwnerID, accID, row.Blob, row.Prefix, row.PrefixSet); err == nil {
						synced++
					}
				}
			}
			if synced > 0 {
				log.Printf("✅ Sinkronisasi Startup: %d sesi dipulihkan/disinkronkan dari MongoDB ke SQLite", synced)
			}
		} else {
			log.Printf("⚠️ Gagal membaca userbot_sessions dari MongoDB: %v", err)
		}

		// 2. Sinkronisasi Catatan (Notes) dari MongoDB Cloud Atlas jika SQLite kosong
		noteCount, _ := s.sqlite.CountAllNotes(ctx)
		if noteCount == 0 {
			log.Println("🔄 Notes SQLite kosong. Mengambil salinan catatan dari MongoDB Cloud...")
			cur, err := s.db.Collection("userbot_notes").Find(ctx, bson.M{})
			if err == nil {
				defer cur.Close(ctx)
				restoredNotes := 0
				for cur.Next(ctx) {
					var doc struct {
						OwnerID        int64  `bson:"owner_id"`
						Name           string `bson:"name"`
						Text           string `bson:"text"`
						Entities       string `bson:"entities"`
						Buttons        string `bson:"buttons"`
						Mode           string `bson:"mode"`
						MediaType      string `bson:"media_type"`
						MediaData      string `bson:"media_data"`
						CacheMessageID int    `bson:"cache_message_id"`
						CacheChannel   string `bson:"cache_channel"`
					}
					if cur.Decode(&doc) == nil {
						nr := databases.NoteRecord{
							OwnerID:        doc.OwnerID,
							Name:           doc.Name,
							Text:           doc.Text,
							Entities:       doc.Entities,
							Buttons:        doc.Buttons,
							Mode:           doc.Mode,
							MediaType:      doc.MediaType,
							MediaData:      doc.MediaData,
							CacheMessageID: doc.CacheMessageID,
							CacheChannel:   doc.CacheChannel,
						}
						if s.sqlite.SaveNote(ctx, nr) == nil {
							restoredNotes++
						}
					}
				}
				log.Printf("✅ Disaster Recovery selesai: %d catatan dipulihkan ke SQLite", restoredNotes)
			}
		}

		// 3. Sinkronisasi Sudo dari MongoDB Cloud Atlas ke SQLite lokal
		curSudo, err := s.db.Collection("userbot_sudo").Find(ctx, bson.M{})
		if err == nil {
			defer curSudo.Close(ctx)
			restoredSudo := 0
			for curSudo.Next(ctx) {
				var row sudoRecord
				if curSudo.Decode(&row) == nil && row.OwnerID > 0 && row.UserID > 0 {
					if s.sqlite.AddSudo(ctx, row.OwnerID, row.UserID) == nil {
						restoredSudo++
					}
				}
			}
			if restoredSudo > 0 {
				log.Printf("✅ Sinkronisasi Startup: %d akses sudo dipulihkan dari MongoDB ke SQLite", restoredSudo)
			}
		} else {
			log.Printf("⚠️ Gagal membaca userbot_sudo dari MongoDB: %v", err)
		}

		// 4. Sinkronisasi Log Status dari MongoDB Cloud Atlas ke SQLite lokal (dipanggil sekali saat startup)
		s.logLoadStatusFromMongo(ctx)

		// 5. Sinkronisasi Riwayat Rute Balasan Log dari MongoDB Cloud Atlas ke SQLite lokal
		s.logLoadRoutesFromMongo(ctx)
	}

	if s.sqlite == nil {
		return
	}

	// Muat seluruh catatan dari SQLite ke Redis cache untuk navigasi inline instan
	s.syncAllNotesToRedis(ctx)

	sessions, err := s.sqlite.GetAllSessions(ctx)
	if err != nil {
		log.Printf("Gagal memuat sesi dari SQLite: %v", err)
		return
	}

	for _, row := range sessions {
		prefix := row.Prefix
		if !row.PrefixSet {
			prefix = "!"
		}
		accID := row.AccountID
		if accID == 0 {
			accID = row.OwnerID
		}
		if accID == 0 {
			accID = row.ChatID
		}
		s.mu.Lock()
		s.prefixes[row.ChatID] = prefix
		s.owners[row.ChatID] = accID
		s.mu.Unlock()
		s.activate(row.ChatID, accID)

		// Staggered Startup Jitter (Anti-CPU-Starvation for 1 vCPU)
		// Membuka 500 akun bertahap ~15 detik tanpa memicu CPU 100% starvation
		time.Sleep(30 * time.Millisecond)
	}
	// Jalankan background reaper khusus membersihkan resource sementara (stale jobs/logins)
	// Reaper TIDAK memutus koneksi MTProto akun aktif (Persistent MTProto 24/7).
	go s.startIdleReaper(ctx)
}

func (s *Service) syncAllNotesToRedis(ctx context.Context) {
	if s.redis == nil || s.sqlite == nil {
		return
	}
	notes, err := s.sqlite.ListAllNotes(ctx)
	if err != nil || len(notes) == 0 {
		return
	}
	synced := 0
	for _, n := range notes {
		var buttons []NoteButton
		if n.Buttons != "" {
			_ = json.Unmarshal([]byte(n.Buttons), &buttons)
		}
		payload := NoteInlinePayload{
			OwnerID: n.OwnerID,
			Name:    n.Name,
			Text:    n.Text,
			Buttons: buttons,
			Mode:    n.Mode,
		}
		if pBytes, err := json.Marshal(payload); err == nil {
			_ = s.redis.Client.Set(ctx, fmt.Sprintf("note:record:%d:%s", n.OwnerID, n.Name), pBytes, 7*24*time.Hour).Err()
		}
		_ = s.redis.Client.Set(ctx, fmt.Sprintf("note:text:%d:%s", n.OwnerID, n.Name), n.Text, 7*24*time.Hour).Err()
		synced++
	}
	if synced > 0 {
		log.Printf("🚀 Sinkronisasi Redis Notes: %d catatan aktif dimuat ke Redis cache", synced)
	}
}

func (s *Service) startIdleReaper(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reapStaleResources()
		}
	}
}

// reapStaleResources bertugas membersihkan resource sementara yang sudah tidak digunakan,
// TANPA memutus koneksi MTProto client aktif (Persistent MTProto 24/7).
func (s *Service) reapStaleResources() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Membersihkan proses login OTP (/create) yang ditinggalkan user > 5 menit
	for chatID, l := range s.logins {
		if l != nil && !l.createdAt.IsZero() && now.Sub(l.createdAt) > 5*time.Minute {
			l.cancel()
			delete(s.logins, chatID)
		}
	}

	// 2. Bounded Cache: Eviction bertahap berbasis TTL dan batas awal kapasitas (2.000)
	s.sudoMu.Lock()
	for k, entry := range s.sudoCache {
		if now.After(entry.expiresAt) {
			delete(s.sudoCache, k)
		}
	}
	// Jika setelah pembersihan TTL ukuran masih melebihi batas konfigurasi awal (2.000),
	// lakukan pemangkasan bertahap (maksimal 20% entri) untuk mencegah lonjakan cache miss serentak ke MongoDB
	if len(s.sudoCache) > 2000 {
		pruned := 0
		for k := range s.sudoCache {
			delete(s.sudoCache, k)
			pruned++
			if pruned >= 400 {
				break
			}
		}
	}
	s.sudoMu.Unlock()
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
	l := &login{cancel: cancel, otp: make(chan string, 1), password: make(chan string, 1), createdAt: time.Now()}
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
	store := &sqliteStorage{sqlite: s.sqlite, db: s.db, chatID: chatID, ownerID: ownerID, key: s.key}
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
	if s.sqlite != nil {
		has, err := s.sqlite.HasSession(ctx, chatID, ownerID)
		if err == nil && has {
			return true, nil
		}
	}
	if s.db != nil {
		count, err := s.db.Collection("userbot_sessions").CountDocuments(ctx, bson.M{
			"$or": []bson.M{
				{"_id": chatID},
				{"owner_id": ownerID},
				{"account_id": ownerID},
			},
			"session": bson.M{"$ne": ""},
		})
		if err == nil && count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) remove(chatID int64) { s.mu.Lock(); delete(s.logins, chatID); s.mu.Unlock() }
func (s *Service) activate(chatID int64, knownOwnerID ...int64) {
	s.mu.Lock()
	if c, ok := s.active[chatID]; ok && c != nil {
		s.lastActive[chatID] = time.Now()
		s.mu.Unlock()
		return
	}
	if s.connecting[chatID] > 0 {
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
	// Admission limit: tolak aktivasi baru jika kapasitas penuh, jangan pernah putus akun aktif
	if len(s.active)+len(s.connecting) >= maxActiveSessions {
		log.Printf("[PoolCap] Kapasitas maksimum tercapai (%d). Menolak aktivasi baru untuk chatID=%d", maxActiveSessions, chatID)
		s.mu.Unlock()
		return
	}
	// Reservasi slot secara atomik dengan status connecting dan generation ID unik
	s.nextGen++
	genID := s.nextGen
	s.connecting[chatID] = genID
	s.accountGens[chatID] = genID
	s.lastActive[chatID] = time.Now()
	clientCtx, clientCancel := context.WithCancel(s.root)
	s.activeCancels[chatID] = clientCancel
	s.accountCtxs[chatID] = clientCtx
	s.mu.Unlock()
	go func() {
		var client *telegram.Client
		defer func() {
			s.mu.Lock()
			// Ownership check: hanya bersihkan connecting jika token generasi masih cocok
			if s.connecting[chatID] == genID {
				delete(s.connecting, chatID)
			}
			// Ownership check: hanya bersihkan active jika client pointer masih cocok
			if s.active[chatID] == client {
				delete(s.active, chatID)
				delete(s.lastActive, chatID)
			}
			// Ownership check: hanya bersihkan cancel, context, dan gen jika generasi masih milik lifecycle ini
			if s.accountGens[chatID] == genID {
				delete(s.activeCancels, chatID)
				delete(s.accountCtxs, chatID)
				delete(s.accountGens, chatID)
			}
			if client != nil {
				delete(s.clientOwners, client)
				delete(s.senderCache, client)
			}
			s.mu.Unlock()
			s.inlineMu.Lock()
			if client != nil {
				delete(s.inlineBots, client)
			}
			s.inlineMu.Unlock()
		}()
		store := &sqliteStorage{sqlite: s.sqlite, db: s.db, chatID: chatID, ownerID: ownerID, key: s.key}
		dispatcher := tg.NewUpdateDispatcher()
		dispatcher.OnNewMessage(func(ctx context.Context, entities tg.Entities, update *tg.UpdateNewMessage) error {
			s.mu.Lock()
			s.lastActive[chatID] = time.Now()
			s.mu.Unlock()
			s.cacheEntities(entities)
			if msg, ok := update.Message.(*tg.Message); ok {
				s.interceptLogMessage(ctx, chatID, client, entities, msg)
			}
			return s.handleUserbotMessage(ctx, chatID, client, entities, update)
		})
		dispatcher.OnNewChannelMessage(func(ctx context.Context, entities tg.Entities, update *tg.UpdateNewChannelMessage) error {
			// Telegram represents supergroup/channel messages with a separate
			// update constructor. Reuse the same outgoing-command dispatcher.
			s.mu.Lock()
			s.lastActive[chatID] = time.Now()
			s.mu.Unlock()
			s.cacheEntities(entities)
			if msg, ok := update.Message.(*tg.Message); ok {
				s.interceptLogMessage(ctx, chatID, client, entities, msg)
			}
			return s.handleUserbotMessage(ctx, chatID, client, entities, &tg.UpdateNewMessage{Message: update.Message})
		})
		updateHandler := &userbotUpdateHandler{
			dispatcher: dispatcher,
			s:          s,
			chatID:     chatID,
			clientRef:  &client,
		}
		client = telegram.NewClient(s.apiID, s.apiHash, telegram.Options{SessionStorage: store, UpdateHandler: updateHandler})
		s.mu.Lock()
		if s.connecting[chatID] == genID {
			delete(s.connecting, chatID)
		}
		if s.accountGens[chatID] == genID {
			s.active[chatID] = client
			if ownerID > 0 {
				s.owners[chatID] = ownerID
				s.clientOwners[client] = ownerID
			}
		}
		s.mu.Unlock()
		err := client.Run(clientCtx, func(ctx context.Context) error {
			self, err := client.Self(ctx)
			if err != nil {
				return err
			}
			s.mu.Lock()
			if s.accountGens[chatID] == genID {
				s.owners[chatID] = self.ID
				s.clientOwners[client] = self.ID
			}
			s.mu.Unlock()
			if store != nil {
				_ = store.setAccount(ctx, self.ID)
			}
			// Pre-warm inline bot resolution so first inline command is fast (staggered & bound to clientCtx).
			s.preWarmInlineBot(clientCtx, client)
			<-ctx.Done()
			return ctx.Err()
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("[Userbot chatID=%d] MTProto selesai/gagal: %v", chatID, err)
		}
	}()
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

	// Fast bailout:
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

	// 1. Jika prefix aktif dan pesan tidak diawali prefix, tolak langsung dalam < 10 nanodetik.
	if prefix != "" {
		if !strings.HasPrefix(msg.Message, prefix) {
			return nil
		}
	} else {
		// 2. Jika prefix == "none" (tanpa prefix):
		// Pesan dari orang lain di grup (!msg.Out) WAJIB ditolak langsung kecuali pengirim terdaftar di Sudo.
		// Ini mencegah obrolan ribuan orang di grup ramai memicu parser.
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

type sqliteStorage struct {
	sqlite  *databases.SQLiteClient
	db      *mongo.Database // optional remote backup
	chatID  int64
	ownerID int64
	key     []byte
}

func (s *sqliteStorage) LoadSession(ctx context.Context) ([]byte, error) {
	if s.sqlite != nil {
		row, err := s.sqlite.GetSessionByChat(ctx, s.chatID)
		if err == nil && row != nil && row.Blob != "" {
			return decrypt(s.key, row.Blob)
		}
	}
	// Fallback ke MongoDB Cloud Atlas jika sesi belum ada di SQLite lokal
	if s.db != nil {
		var mRow record
		err := s.db.Collection("userbot_sessions").FindOne(ctx, bson.M{
			"$or": []bson.M{
				{"_id": s.chatID},
				{"owner_id": s.ownerID},
			},
			"session": bson.M{"$ne": ""},
		}).Decode(&mRow)
		if err == nil && mRow.Blob != "" {
			accID := mRow.AccountID
			if accID == 0 {
				accID = mRow.OwnerID
			}
			if accID == 0 {
				accID = mRow.ChatID
			}
			if s.sqlite != nil {
				_ = s.sqlite.SaveSession(ctx, mRow.ChatID, mRow.OwnerID, accID, mRow.Blob, mRow.Prefix, mRow.PrefixSet)
			}
			return decrypt(s.key, mRow.Blob)
		}
	}
	return nil, session.ErrNotFound
}

func (s *sqliteStorage) StoreSession(ctx context.Context, data []byte) error {
	blob, err := encrypt(s.key, data)
	if err != nil {
		return err
	}
	if s.sqlite != nil {
		if err := s.sqlite.SaveSession(ctx, s.chatID, s.ownerID, 0, blob, "!", false); err != nil {
			return err
		}
	}
	// Asynchronous offsite backup to MongoDB
	if s.db != nil {
		go func(cid, oid int64, b string) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_sessions").UpdateByID(bgCtx, cid, bson.M{
				"$set": bson.M{"owner_id": oid, "session": b, "updated_at": time.Now().UTC()},
			}, options.Update().SetUpsert(true))
		}(s.chatID, s.ownerID, blob)
	}
	return nil
}

func (s *sqliteStorage) setAccount(ctx context.Context, id int64) error {
	if s.sqlite != nil {
		row, err := s.sqlite.GetSessionByChat(ctx, s.chatID)
		if err == nil && row != nil {
			_ = s.sqlite.SaveSession(ctx, s.chatID, row.OwnerID, id, row.Blob, row.Prefix, row.PrefixSet)
		}
	}
	if s.db != nil {
		go func(cid, accID int64) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_sessions").UpdateByID(bgCtx, cid, bson.M{
				"$set": bson.M{"account_id": accID, "updated_at": time.Now().UTC()},
			})
		}(s.chatID, id)
	}
	return nil
}

func (s *sqliteStorage) setPrefix(ctx context.Context, prefix string, configured bool) error {
	if s.sqlite != nil {
		if err := s.sqlite.UpdatePrefix(ctx, s.chatID, prefix); err != nil {
			return err
		}
	}
	if s.db != nil {
		go func(cid int64, pfx string, conf bool) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_sessions").UpdateByID(bgCtx, cid, bson.M{
				"$set": bson.M{"prefix": pfx, "prefix_configured": conf, "updated_at": time.Now().UTC()},
			})
		}(s.chatID, prefix, configured)
	}
	return nil
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
	switch upd := u.(type) {
	case *tg.UpdateShortMessage:
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

	default:
		return h.dispatcher.Handle(ctx, u)
	}
}
