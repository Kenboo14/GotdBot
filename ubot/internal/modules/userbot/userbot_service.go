package userbot

import (
	"assterisk/ubot/internal/databases"
	"assterisk/ubot/internal/models"
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var idleTimeoutMinutes = 30
var maxActiveSessions = 100
var sessionHeartbeatSeconds = 30

func init() {
	if v, err := strconv.Atoi(os.Getenv("IDLE_TIMEOUT_MINUTES")); err == nil && v > 0 {
		idleTimeoutMinutes = v
	}
	if v, err := strconv.Atoi(os.Getenv("MAX_ACTIVE_SESSIONS")); err == nil && v > 0 {
		maxActiveSessions = v
	}
	if v, err := strconv.Atoi(os.Getenv("SESSION_HEARTBEAT_SECONDS")); err == nil && v >= 15 {
		sessionHeartbeatSeconds = v
	}
}

type AccountSessionState string

const (
	StateConnecting     AccountSessionState = "CONNECTING"
	StateConnected      AccountSessionState = "CONNECTED"
	StateReconnecting   AccountSessionState = "RECONNECTING"
	StateSuspected      AccountSessionState = "SUSPECTED"
	StateVerifying      AccountSessionState = "VERIFYING"
	StateSessionInvalid AccountSessionState = "SESSION_INVALID"
	StateStopped        AccountSessionState = "STOPPED"
)

type rpcErrorMiddleware struct {
	chatID  int64
	genID   uint64
	service *Service
}

func (m *rpcErrorMiddleware) Handle(next tg.Invoker) telegram.InvokeFunc {
	return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
		// Flood control guard: jika akun sedang dalam periode cooldown FLOOD_WAIT Telegram,
		// tahan request sejenak hingga cooldown selesai untuk mencegah eskalasi penalti ban Telegram.
		if wait := m.service.getFloodWaitRemaining(m.chatID); wait > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			// Anti-thundering-herd: Jika banyak RPC serentak bangun setelah cooldown yang sama,
			// mereka akan menembak MTProto pada millisecond yang persis sama dan
			// langsung memicu FLOOD_WAIT baru. Jitter acak [0, 250ms) mencegah burst tersebut.
			if jitter := randJitterMs(250); jitter > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(jitter):
				}
			}
		}

		err := next.Invoke(ctx, input, output)
		if err != nil {
			if flood, ok := tgerr.AsFloodWait(err); ok {
				m.service.recordFloodWait(m.chatID, flood)
			} else if isSessionRevokedError(err) {
				// Menjalankan cleanup secara asynchronous agar tidak memblokir jalur Invoke RPC
				go m.service.handleSessionRevoked(m.chatID, m.genID, err)
			}
		}
		return err
	}
}

type Service struct {
	root                context.Context
	sqlite              *databases.SQLiteClient
	db                  *mongo.Database
	redis               *databases.Redis
	apiID               int
	apiHash             string
	key                 []byte
	botToken            string
	logBotToken         string
	logBotID            int64
	logBotUsername      string
	startedLogBots      sync.Map // key: [2]int64{userbotID, logBotID} -> true
	logStatusCache      sync.Map // key: int64 (ownerID) -> bool
	ownerUsernames      sync.Map // key: int64 (ownerID) -> lowercase username without @
	userAccessHashes    sync.Map // key: int64 (userID) -> int64 (accessHash)
	channelAccessHashes sync.Map // key: int64 (channelID) -> int64 (accessHash)
	lastLogBotMsgID     sync.Map // key: int64 (ownerID) -> int (MTProto msg ID terbaru dari LogBot)
	floodCooldowns      sync.Map // key: int64 (chatID) -> time.Time (cooldownUntil)
	albumCache          sync.Map // key: int (firstMsgID) -> []int (semua album msgIDs)
	cmdSem              chan struct{} // semaphore pembatas concurrent Redis command jobs (max 32)
	inlineMu            sync.RWMutex
	botUsernameCache    string
	inlineBots          map[*telegram.Client]*tg.InputUser
	mu                  sync.Mutex
	logins              map[int64]*login
	active              map[int64]*telegram.Client
	connecting          map[int64]uint64                     // guard terhadap double activation & reservasi slot (chatID -> genID)
	activeCancels       map[int64]context.CancelFunc         // cancel func untuk graceful shutdown
	accountCtxs         map[int64]context.Context            // chatID -> clientCtx (account lifecycle context)
	accountGens         map[int64]uint64                     // chatID -> token kepemilikan generasi lifecycle
	nextGen             uint64                               // counter generasi unik untuk lifecycle
	accountStates       map[int64]AccountSessionState        // chatID -> status lifecycle event-driven
	lastActive          map[int64]time.Time                  // chatID -> waktu aktivitas terakhir
	lastUpdate          map[int64]time.Time                  // chatID -> waktu terakhir incoming update diterima
	lastVerified        map[int64]time.Time                  // chatID -> waktu terakhir verifikasi pesan 'Check'
	verifying           map[int64]bool                       // chatID -> guard anti-double-verification
	verifSem            chan struct{}                        // semaphore pembatas concurrent Check ke AiotdCaches (max 2)
	heartbeatSem        chan struct{}                        // pembatas RPC heartbeat sesi aktif
	owners              map[int64]int64                      // chatID -> ownerID
	clientOwners        map[*telegram.Client]int64           // client -> ownerID
	senderCache         map[*telegram.Client]*message.Sender // cached message.Sender per client
	prefixes            map[int64]string
	broadcasts          map[int64]*broadcastJob
	panelBroadcasts     map[int64]*panelBroadcastJob
	sudoMu              sync.RWMutex
	sudoCache           map[string]sudoCacheEntry // "ownerID:userID" -> sudoCacheEntry dengan TTL
	broadcastSem        chan struct{}             // Semaphore untuk membatasi concurrent gcast
}

// publishSessionState mengirimkan perubahan lifecycle akun sebagai event
// terpisah dari pesan Telegram. Consumer seperti gateway atau WebSocket
// gateway dapat memakai stream ini untuk update real-time tanpa polling.
// Stream ini sengaja tidak memakai from:ubot karena stream tersebut ditujukan
// untuk pesan yang memang harus dikirim ke chat Telegram owner.
func (s *Service) publishSessionState(chatID, ownerID int64, state AccountSessionState, reason string) {
	if s.redis == nil || chatID == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.redis.To(ctx, "session:events", &models.PFromService{
		ChatID:  chatID,
		OwnerID: ownerID,
		State:   string(state),
		Type:    "session_status",
		Text:    reason,
	}); err != nil {
		slog.Error("failed to publish session state",
			slog.Int64("chat_id", chatID),
			slog.Int64("owner_id", ownerID),
			slog.String("state", string(state)),
			slog.String("error", err.Error()),
		)
	}
}

// setAccountState memperbarui state hanya untuk lifecycle generation yang
// masih aktif, lalu mengirim event ketika state benar-benar berubah.
func (s *Service) setAccountState(chatID int64, genID uint64, state AccountSessionState, reason string) {
	s.mu.Lock()
	if genID != 0 && s.accountGens[chatID] != genID {
		s.mu.Unlock()
		return
	}
	previous := s.accountStates[chatID]
	s.accountStates[chatID] = state
	ownerID := s.owners[chatID]
	s.mu.Unlock()

	if previous != state {
		s.publishSessionState(chatID, ownerID, state, reason)
	}
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
		root:            root,
		sqlite:          sqlite,
		db:              db,
		redis:           redis,
		apiID:           apiID,
		apiHash:         apiHash,
		key:             key,
		botToken:        botToken,
		logBotToken:     logBotToken,
		logins:          make(map[int64]*login),
		active:          make(map[int64]*telegram.Client),
		connecting:      make(map[int64]uint64),
		activeCancels:   make(map[int64]context.CancelFunc),
		accountCtxs:     make(map[int64]context.Context),
		accountGens:     make(map[int64]uint64),
		accountStates:   make(map[int64]AccountSessionState),
		lastActive:      make(map[int64]time.Time),
		lastUpdate:      make(map[int64]time.Time),
		lastVerified:    make(map[int64]time.Time),
		verifying:       make(map[int64]bool),
		verifSem:        make(chan struct{}, 2), // hemat resource, max 2 concurrent Check ke AiotdCaches
		heartbeatSem:    make(chan struct{}, 16),
		owners:          make(map[int64]int64),
		clientOwners:    make(map[*telegram.Client]int64),
		senderCache:     make(map[*telegram.Client]*message.Sender),
		prefixes:        make(map[int64]string),
		broadcasts:      make(map[int64]*broadcastJob),
		panelBroadcasts: make(map[int64]*panelBroadcastJob),
		inlineBots:      make(map[*telegram.Client]*tg.InputUser),
		sudoCache:       make(map[string]sudoCacheEntry),
		broadcastSem:    make(chan struct{}, 5), // maksimal 5 gcast bersamaan untuk hemat CPU & RAM
		cmdSem:          make(chan struct{}, 32), // batas concurrency perintah background
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
	// Callback MTProto tetap menjadi jalur utama. Heartbeat hanya menjalankan
	// RPC ringan sebagai fallback saat koneksi transport masih hidup tetapi
	// AuthKey sudah dicabut dari perangkat lain.
	go s.startSessionHeartbeatWorker(ctx)
}

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
		slog.Warn("pool capacity reached, rejecting activation", slog.Int("max_active", maxActiveSessions), slog.Int64("chat_id", chatID))
		s.mu.Unlock()
		return
	}
	// Reservasi slot secara atomik dengan status connecting dan generation ID unik
	s.nextGen++
	genID := s.nextGen
	s.connecting[chatID] = genID
	s.accountGens[chatID] = genID
	// State CONNECTING dipancarkan sebagai event lifecycle, bukan dipolling.
	s.accountStates[chatID] = StateConnecting
	initialOwnerID := ownerID
	s.lastActive[chatID] = time.Now()
	clientCtx, clientCancel := context.WithCancel(s.root)
	s.activeCancels[chatID] = clientCancel
	s.accountCtxs[chatID] = clientCtx
	s.mu.Unlock()
	s.publishSessionState(chatID, initialOwnerID, StateConnecting, "MTProto client sedang menghubungkan akun")
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
				if peerUser, ok := msg.PeerID.(*tg.PeerUser); ok && peerUser.UserID == 777000 {
					s.triggerImmediateVerification(chatID)
				}
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
		client = telegram.NewClient(s.apiID, s.apiHash, telegram.Options{
			SessionStorage: store,
			UpdateHandler:  updateHandler,
			Middlewares: []telegram.Middleware{
				&rpcErrorMiddleware{chatID: chatID, genID: genID, service: s},
			},
			OnDead: func(err error) {
				if isSessionRevokedError(err) {
					go s.handleSessionRevoked(chatID, genID, err)
				}
			},
			OnConnectionState: func(state telegram.ConnectionState) {
				s.mu.Lock()
				// Ownership check: abaikan event jika generasi client sudah tidak cocok
				if s.accountGens[chatID] != genID {
					s.mu.Unlock()
					return
				}
				switch state {
				case telegram.ConnectionStateReady:
					wasSuspected := (s.accountStates[chatID] == StateSuspected || s.accountStates[chatID] == StateReconnecting)
					s.accountStates[chatID] = StateConnected
					ownerID := s.owners[chatID]
					s.mu.Unlock()
					s.publishSessionState(chatID, ownerID, StateConnected, "Koneksi MTProto siap")
					// Jika sebelumnya terputus (suspected), lakukan verifikasi saat koneksi sudah READY
					if wasSuspected {
						s.triggerImmediateVerification(chatID)
					}
					return
				case telegram.ConnectionStateDisconnected:
					// Disconnect bukan bukti sesi dicabut. Tandai event sementara dan
					// biarkan gotd melakukan reconnect atau mengembalikan auth error.
					s.accountStates[chatID] = StateSuspected
					ownerID := s.owners[chatID]
					s.mu.Unlock()
					s.publishSessionState(chatID, ownerID, StateSuspected, "Koneksi MTProto terputus; menunggu hasil reconnect")
					return
				case telegram.ConnectionStateConnecting:
					if s.accountStates[chatID] != StateSuspected {
						s.accountStates[chatID] = StateConnecting
					}
					ownerID := s.owners[chatID]
					s.mu.Unlock()
					s.publishSessionState(chatID, ownerID, StateConnecting, "MTProto sedang reconnect")
					return
				}
				s.mu.Unlock()
			},
		})
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
				s.accountStates[chatID] = StateConnected
			}
			s.mu.Unlock()
			s.publishSessionState(chatID, self.ID, StateConnected, "Sesi Telegram berhasil diautentikasi")
			if store != nil {
				_ = store.setAccount(ctx, self.ID)
			}
			// Pre-warm inline bot resolution so first inline command is fast (staggered & bound to clientCtx).
			s.preWarmInlineBot(clientCtx, client)
			// Status broadcast disimpan persisten. Jika server restart saat
			// status masih Berjalan, lanjutkan dari checkpoint dan waktu jatuh
			// tempo yang tersimpan, bukan memulai ulang dari target pertama.
			go s.resumePanelBroadcast(client, chatID, self.ID)

			now := time.Now()
			s.mu.Lock()
			s.lastUpdate[chatID] = now
			s.lastVerified[chatID] = now
			s.mu.Unlock()

			// Menunggu hingga context client dihentikan atau koneksi terputus dari MTProto
			<-ctx.Done()
			return ctx.Err()
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("mtproto finished or failed", slog.Int64("chat_id", chatID), slog.String("error", err.Error()))
			if isSessionRevokedError(err) {
				go s.handleSessionRevoked(chatID, genID, err)
				return
			}

			// Jika koneksi terputus karena gangguan sementara (jaringan/timeout):
			// JANGAN hapus sesi! Jadwalkan reconnect otomatis agar akun tetap online 24/7.
			s.mu.Lock()
			isStillCurrent := (s.accountGens[chatID] == genID)
			delete(s.active, chatID)
			s.mu.Unlock()

			if isStillCurrent && clientCtx.Err() == nil {
				slog.Warn("mtproto disconnected temporarily, scheduling auto-reconnect", slog.Int64("chat_id", chatID), slog.String("error", err.Error()))
				s.publishSessionState(chatID, ownerID, StateReconnecting, err.Error())
				time.Sleep(3 * time.Second)
				s.activate(chatID, ownerID)
			}
		}
	}()
}
