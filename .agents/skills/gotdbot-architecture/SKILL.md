---
name: gotdbot-architecture
description: >-
  Architectural blueprint, coding standards, and high-performance design principles
  for the GotdBot project. Use this skill whenever refactoring, creating new files,
  moving code, splitting packages, optimizing memory/goroutines, or reviewing structure.
  Enforces senior-level idiomatic Go layout, separation of concerns, goroutine bounding,
  GC friendliness, zero-bottleneck microservices, and clean architecture.
---

# GotdBot Architecture & Engineering Standard

> **Core Philosophy:** **Optimize, Efisien, Easy Using**  
> **Target Scale:** Ribuan concurrent user/sessions tanpa bottleneck, low RAM/CPU footprint, zero goroutine leaks, dan production-grade reliability.

--------------------------------------------------------------------------------

## 0. Core Goals & Engineering Principles

Setiap baris kode, refaktor, dan desain arsitektur di GotdBot WAJIB mematuhi 4 pilar berikut:

### 1. Optimize (Performa & Resource Efficiency)
- **Zero Allocations di Hot Path:** Hindari alokasi objek repetitif di loop MTProto updates dan broadcast stream. Gunakan `strings.Builder`, reuse slice kapasitas (`make([]T, 0, cap)`), dan pertimbangkan `sync.Pool` untuk buffer serialization/decoding.
- **GC-Friendly Design:** Menjaga alokasi heap serendah mungkin agar GC cycles (`num_gc`) minim. Memanfaatkan tuning runtime Go (`GOMEMLIMIT=1100MiB`, `GOGC=50`) secara optimal tanpa memicu OOM kiler.
- **Fast Path Caching:** Akses state dan metadata menggunakan lock yang granular (`sync.RWMutex` per-domain atau `sync.Map` untuk read-heavy caches seperti AccessHash, FloodCooldown, Usernames).

### 2. Efisien (Goroutine Governance & Concurrency Control)
- **No Unbounded Goroutines:** DILARANG menjalankan `go func()` liar tanpa lifecycle/bounding. Setiap goroutine harus terikat dengan `context.Context` (bisa di-cancel) atau worker pool/semaphore (`cmdSem`, bounded worker channels).
- **Graceful Shutdown & Leak Prevention:** Semua background worker (Redis listener, heartbeat, broadcast runner) wajib monitor `<-ctx.Done()` dan membersihkan resource seketika.
- **Anti-Thundering-Herd:** Terapkan jittering acak (misal `randJitterMs(250)`) pada reconnection, retry database, dan recovery flood wait untuk mencegah traffic spike serentak ke MTProto/DB.

### 3. Easy Using (Developer Experience & User Experience)
- **Maintainable & Predictable:** Struktur modular yang intuitif. Membuka folder domain (`panel/`, `session/`, `broadcast/`) langsung memperlihatkan tanggung jawab domain tersebut tanpa harus scroll ribuan baris file god-object.
- **Self-Documenting Code:** Signature fungsi jelas, single-responsibility, interface ramping, dan penanganan error yang informatif.
- **Konsistensi UI Telegram:** Layout keyboard navigasi user (`ReplyKeyboard`) tetap stabil dan mudah dipahami, sementara tampilan output pesan (`HTML`) bersih, modern, dan padat informasi.

### 4. Microservice Purity & Production-Grade Pipeline
- **Decoupled Architecture:** `gateway` (Bot Interface & Orchestrator) dan `ubot` (MTProto Engine) berkomunikasi asinkron via Redis Streams / PubSub dengan kontrak payload yang ketat (`shared` schemas).
- **Graceful Backpressure:** Jika ubot overload atau database lambat, queue broker menampung sementara; service tidak boleh panic atau hang.

--------------------------------------------------------------------------------

## 1. Project Layout

GotdBot is a **multi-binary Go project** with two services (`gateway`, `ubot`)
and an optional shared types module.

```
GotdBot/                            ← repository root
├── gateway/                        ← service 1: Telegram Bot Panel (MTProto gateway)
│   ├── cmd/
│   │   └── gateway/
│   │       └── main.go             ← thin entrypoint only
│   ├── internal/                   ← compiler-enforced private packages
│   │   ├── app/                    ← application wiring & lifecycle
│   │   │   ├── app.go
│   │   │   └── shutdown.go
│   │   ├── panel/                  ← domain: control panel
│   │   │   ├── handler.go          ← thin bot handler (routing + input validation)
│   │   │   ├── service.go          ← business logic + Service interface
│   │   │   ├── state.go            ← panel state machine (all mutex/maps isolated here)
│   │   │   ├── menu.go             ← ShowMainMenu, navigation logic
│   │   │   ├── status.go           ← ShowStatus, dashboard
│   │   │   ├── list.go             ← list CRUD operations
│   │   │   ├── group.go            ← group CRUD operations
│   │   │   ├── broadcast.go        ← broadcast monitor panel
│   │   │   ├── media.go            ← media batch handling
│   │   │   ├── callback.go         ← inline callback routing
│   │   │   └── *_test.go
│   │   ├── userbot/                ← domain: userbot session management
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── session.go          ← session wizard / auth flow
│   │   │   ├── server.go           ← server dashboard
│   │   │   └── *_test.go
│   │   ├── token/                  ← domain: token & license management
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── flow.go             ← token claim / restore flow
│   │   │   └── *_test.go
│   │   ├── user/                   ← domain: user onboarding / /start
│   │   │   ├── handler.go
│   │   │   └── service.go
│   │   ├── ui/                     ← presentation layer (ZERO business logic)
│   │   │   ├── text.go             ← HTML message templates
│   │   │   ├── rich.go             ← rich text builders (tables, status cards)
│   │   │   ├── reply_keyboard.go   ← ReplyKeyboardMarkup definitions
│   │   │   └── inline_keyboard.go  ← InlineKeyboardMarkup definitions
│   │   └── infra/                  ← infrastructure adapters
│   │       ├── telegram/
│   │       │   ├── bot.go          ← gotd/td bot wrapper (current botgotd/)
│   │       │   ├── dispatcher.go
│   │       │   ├── models.go
│   │       │   └── peers.go
│   │       ├── store/              ← data access layer
│   │       │   ├── manager.go      ← DataManager (SQLite + Mongo orchestrator)
│   │       │   ├── sqlite.go
│   │       │   ├── mongodb.go
│   │       │   ├── models.go       ← DB-specific struct types
│   │       │   └── *_test.go
│   │       ├── broker/
│   │       │   └── redis.go        ← Redis Streams pub/sub
│   │       └── config/
│   │           └── config.go
│   ├── go.mod                      ← module: assterisk/gateway
│   └── Dockerfile
│
├── ubot/                           ← service 2: Userbot MTProto Engine
│   ├── cmd/
│   │   └── ubot/
│   │       └── main.go
│   ├── internal/
│   │   ├── app/
│   │   │   ├── app.go
│   │   │   ├── shutdown.go
│   │   │   └── pprof.go
│   │   ├── session/                ← domain: MTProto session lifecycle
│   │   │   ├── service.go          ← Service struct + NewService
│   │   │   ├── manager.go          ← connect, restore, disconnect logic
│   │   │   ├── crypto.go           ← session encryption/decryption
│   │   │   ├── state.go            ← AccountSessionState, flood control
│   │   │   ├── auth.go             ← auth flow handling (phone, code, 2FA)
│   │   │   └── *_test.go
│   │   ├── broadcast/              ← domain: broadcast engine
│   │   │   ├── service.go
│   │   │   ├── panel.go            ← broadcast panel messages
│   │   │   ├── repo.go
│   │   │   └── *_test.go
│   │   ├── command/                ← domain: userbot chat commands
│   │   │   ├── router.go           ← command dispatch
│   │   │   ├── log.go
│   │   │   ├── notes.go
│   │   │   ├── broadcast.go
│   │   │   ├── eval.go
│   │   │   ├── inline.go
│   │   │   ├── blacklist.go
│   │   │   ├── shell.go
│   │   │   ├── sudo.go
│   │   │   ├── prefix.go
│   │   │   ├── code.go
│   │   │   └── *_test.go
│   │   └── infra/
│   │       ├── store/
│   │       │   ├── sqlite.go
│   │       │   └── mongodb.go
│   │       ├── broker/
│   │       │   └── redis.go
│   │       └── config/
│   │           └── config.go
│   ├── go.mod                      ← module: assterisk/ubot
│   └── Dockerfile
│
├── shared/                         ← (optional) shared contract types
│   ├── message.go                  ← PToService, PFromService, MessageEntity
│   └── go.mod                      ← module: assterisk/shared
│
├── docker-compose.yaml
├── docker-compose.dev.yaml
├── .github/workflows/
├── .env.example
└── .gitignore
```

### Layout Rules

| Rule | Detail |
|------|--------|
| **Entrypoint** | `cmd/<binary>/main.go` — thin, only signal handling + `app.Run(ctx)` |
| **Private code** | Everything under `internal/` — Go compiler enforces no external import |
| **Domain packages** | One package per business domain: `panel/`, `userbot/`, `token/`, `session/`, `broadcast/`, `command/` |
| **Infrastructure** | `infra/` holds adapters (database, redis, telegram, config) — separate from domain logic |
| **Presentation** | `ui/` holds ALL text templates, keyboard definitions — ZERO business logic |
| **No `applications/`** | This directory name does not exist in Go conventions |
| **No `controllers/`** | Go does not have "controllers". Use domain package names instead |
| **No `models/`** | Types belong in the package that uses them. DB structs in `store/models.go`, domain types in `<domain>/types.go` |
| **No `utils/`** | Move helper functions into the domain that uses them, or create a specific-named package (e.g., `strutil`) |
| **`pkg/`** | Only for code intended to be importable by external projects. If nothing qualifies, do not create it |

--------------------------------------------------------------------------------

## 2. Package Naming

### MUST follow

- Package names are **lowercase, single-word, no underscores, no mixedCaps**
- Name describes **what it does**, not what it contains
- Never use generic names: `utils`, `helpers`, `common`, `models`, `types`, `base`, `shared` (exception: the top-level `shared/` module for cross-service contracts)

### Naming Map (current → target)

| Current | Target | Reason |
|---------|--------|--------|
| `applications/controllers` | `internal/panel`, `internal/userbot`, `internal/token`, `internal/user` | Split by domain |
| `applications/assets` | `internal/ui` | "ui" describes purpose; "assets" is generic |
| `applications/botgotd` | `internal/infra/telegram` | Infrastructure adapter |
| `applications/broker` | `internal/infra/broker` | Infrastructure adapter |
| `applications/databases` | `internal/infra/store` | "store" describes behavior |
| `applications/config` | `internal/infra/config` | Infrastructure |
| `applications/middleware` | `internal/infra/telegram/middleware.go` | Part of telegram infra |
| `applications/models` | Inline in domain packages or `shared/` module | Types belong where used |
| `applications/utils` | Inline in domain packages | No junk drawers |
| `applications/modules/userbot` | `internal/session` + `internal/command` | Split by concern |
| `applications/modules/broadcast` | `internal/broadcast` | Domain package |

--------------------------------------------------------------------------------

## 3. File Rules

### Size Limits

| Metric | Limit | Action if exceeded |
|--------|-------|--------------------|
| Lines per file | **400 LOC max** | Split by sub-concern |
| Functions per file | **10-15 max** | Split by responsibility |
| Struct methods per file | **8-10 max** | Group related methods into separate files |

### Naming Conventions

| File | Contains |
|------|----------|
| `service.go` | Service struct definition, constructor (`NewService`), and the service interface |
| `handler.go` | Thin bot/HTTP handler: routing, input validation, delegates to service |
| `state.go` | All `sync.Mutex`/`sync.RWMutex` fields, maps, and their accessor methods |
| `types.go` | Package-scoped type definitions (if not in service.go) |
| `*_test.go` | Tests for the corresponding file |

### File Organization Within a Domain Package

```go
// panel/service.go — THE entry point for understanding this domain

// PanelService defines the business operations for the control panel.
type PanelService interface {
    ShowMainMenu(ctx context.Context, chatID, userID int64, name string) error
    ShowStatus(ctx context.Context, chatID, userID int64) error
    // ...
}

// Service implements PanelService.
type Service struct {
    bot    telegram.BotSender   // interface, not concrete
    store  store.ConfigReader   // interface, not concrete
    broker broker.Publisher     // interface, not concrete
    state  *State               // from state.go
    ui     *ui.Builder          // from ui package
}

func NewService(bot telegram.BotSender, store store.ConfigReader, ...) *Service { ... }
```

```go
// panel/handler.go — THIN routing layer

func (h *Handler) HandleMessage(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
    // 1. Extract input
    // 2. Validate
    // 3. Delegate to h.service.ShowMainMenu(...)
    // NEVER: query DB, format strings, manage state here
}
```

```go
// panel/state.go — ALL mutex/maps isolated here

type State struct {
    mu     sync.RWMutex
    states map[int64]string
}

func NewState() *State { ... }
func (s *State) Set(chatID int64, state string) { ... }
func (s *State) Get(chatID int64) string { ... }
func (s *State) Clear(chatID int64) { ... }
```

--------------------------------------------------------------------------------

## 4. Separation of Concerns

### Three-Layer Rule

Every domain MUST have clean separation:

```
Handler (thin)  →  Service (business logic)  →  Store/Repo (data access)
    ↓                    ↓                            ↓
 Routing only      Domain rules,              SQL, Mongo, Redis
 Input parsing     orchestration,             queries only
 Delegation        state transitions
```

### What goes WHERE

| Layer | MUST contain | MUST NOT contain |
|-------|-------------|-----------------|
| **handler.go** | Route matching, input extraction, call service, return response | DB queries, string formatting, mutex, business rules |
| **service.go** | Business logic, orchestration, error decisions | Direct DB access (use store interface), HTML formatting |
| **state.go** | Mutex, maps, atomic state operations | Business logic, I/O |
| **ui/ package** | HTML template strings, keyboard builders | Business logic, DB access, state management |
| **store/ package** | SQL/Mongo queries, data mapping | Business logic, Telegram API calls |

### Anti-patterns to REJECT

```go
// ❌ REJECT: Handler doing everything
func (h *Handler) ShowStatus(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
    cfg := h.db.GetUserConfig(ctx, userID)          // direct DB access
    gCount, lCount := h.db.CountGroupsAndLists(ctx, userID) // more DB
    skipFw := "Mati"                                 // business logic
    if cfg.SkipFloodWait { skipFw = "Aktif" }
    text := fmt.Sprintf("<b>Status</b>\n...")         // UI formatting
    b.SendMessage(ctx, &botgotd.SendMessageParams{   // telegram call
        Text: text,
    })
}

// ✅ ACCEPT: Handler delegates to service
func (h *Handler) ShowStatus(ctx context.Context, b *botgotd.Bot, chatID, userID int64) {
    h.service.ShowStatus(ctx, chatID, userID)
}

// ✅ Service does orchestration
func (s *Service) ShowStatus(ctx context.Context, chatID, userID int64) error {
    cfg, err := s.store.GetUserConfig(ctx, userID)
    if err != nil { return fmt.Errorf("get config: %w", err) }
    counts, err := s.store.CountGroupsAndLists(ctx, userID)
    if err != nil { return fmt.Errorf("count: %w", err) }
    text := s.ui.StatusDashboard(cfg, counts)
    return s.bot.Send(ctx, chatID, text, "HTML", ui.StatusMenuKeyboard())
}
```

--------------------------------------------------------------------------------

## 5. Interface-Driven Design

### Rule: Accept interfaces, return structs

Every cross-package dependency MUST be an interface defined at the **consumer** side.

```go
// panel/service.go — consumer defines what it needs
type ConfigReader interface {
    GetUserConfig(ctx context.Context, userID int64) (store.UserConfig, error)
    CountGroupsAndLists(ctx context.Context, userID int64) (int, int, error)
}

type BotSender interface {
    SendMessage(ctx context.Context, params *SendMessageParams) (*Message, error)
    EditMessage(ctx context.Context, params *EditMessageParams) error
    DeleteMessage(ctx context.Context, params *DeleteMessageParams) error
}
```

### Why

- Unit tests can use mock implementations
- Packages don't import concrete infrastructure
- Swapping SQLite for Postgres only changes `store/` — zero domain changes

--------------------------------------------------------------------------------

## 6. Error Handling

| Pattern | Rule |
|---------|------|
| **Wrapping** | Always wrap: `fmt.Errorf("show status for user %d: %w", userID, err)` |
| **Propagation** | Services return `error`; handlers decide how to report to user |
| **Logging** | Use structured logger (`log/slog` or `zap`); no `log.Printf` with emoji |
| **Fatal** | Only in `main.go` or `app.go` during startup; never in domain code |
| **Silent swallow** | Forbidden. If error is intentionally ignored, comment why: `_ = cleanup() // best-effort, non-critical` |

--------------------------------------------------------------------------------

## 7. Concurrency & Goroutine Governance

### The "No-Unbounded-Goroutine" Law
Setiap goroutine di GotdBot harus memiliki lifecycle yang terkendali:
1. **Context-Bounded:** Dilarang menggunakan `go func() { ... }()` tanpa koordinasi `context.Context` atau `sync.WaitGroup`.
2. **Semaphore-Guarded Jobs:** Pekerjaan berat atau I/O intensif (eksekusi perintah redis, broadcast burst, verifikasi status MTProto) wajib dibatasi menggunakan semaphore channel (misal `chan struct{}` bertampung 16–32) atau fixed worker pool.
3. **Goroutine Leak Prevention:** Seluruh loop worker atau listener Redis wajib mendengarkan `select { case <-ctx.Done(): return }`.
4. **Leak Detection in Tests:** Gunakan `uber-go/goleak` pada unit dan integration test untuk memastikan tidak ada goroutine tertinggal setelah test selesai.

### Mutex Isolation Rule

All `sync.Mutex`/`sync.RWMutex` + their protected maps MUST live in a
dedicated `state.go` file within the domain package, wrapped in a `State`
struct with clean accessor methods.

```go
// ❌ REJECT: 8 mutex fields scattered in one Handler struct
type Handler struct {
    broadcastMu    sync.Mutex
    panelStatesMu  sync.RWMutex
    progressMu     sync.Mutex
    // ... 5 more
}

// ✅ ACCEPT: Each concern has its own state type
// panel/state.go
type PanelState struct {
    mu     sync.RWMutex
    states map[int64]string
}

// panel/progress.go or panel/state.go
type ProgressTracker struct {
    mu       sync.Mutex
    messages map[int64]int
}
```

--------------------------------------------------------------------------------

## 7.1. Memory, GC & High-Scale Optimization

Target GotdBot adalah menangani **ribuan concurrent MTProto sessions** dan ribuan user dengan footprint RAM/CPU yang sangat bersahabat:

### Memory & GC Guidelines
1. **GOMEMLIMIT & GOGC:**
   - Default production setting: `GOMEMLIMIT=1100MiB` dan `GOGC=50`.
   - Mengapa? Setting ini memberitahu Go runtime untuk memicu GC lebih awal sebelum menyentuh limit container VPS, mencegah crash OOM-killer dari kernel Linux.
2. **Minimalkan Heap Allocations di Hot Path:**
   - Gunakan `strings.Builder` (dengan `Grow(n)` jika ukuran terprediksi) daripada konkatenasi `+` berulang.
   - Gunakan `sync.Pool` untuk buffer byte, serializer, atau objek JSON parser temporer yang dibuat jutaan kali saat broadcast.
   - Pre-allocate kapasitas slice: `make([]int64, 0, expectedLen)`.
3. **Lightweight MTProto Session Management:**
   - Akun idle yang tidak ada aktivitas selama `IDLE_TIMEOUT_MINUTES` diturunkan statusnya ke hibernasi/sleeping jika perlu.
   - Heartbeat verification bertahap dengan algoritma prioritas (hanya cek akun bermasalah atau akun yang lama tidak diverifikasi).
4. **Pprof Monitoring Built-in:**
   - Endpoint pprof (`127.0.0.1:6060/debug/pprof/`) tetap aktif di localhost untuk memudahkan profiling real-time memory leak, goroutine profile, dan CPU flamegraph kapan saja tanpa restart.

--------------------------------------------------------------------------------

## 7.2. Microservice Pipeline & Redis Streams Standards

Target arsitektur microservice GotdBot adalah **Zero-Bottleneck, High-Throughput, dan Resilient** untuk ribuan user:

```
[ Telegram Updates ]
        │
        ▼
   ┌──────────┐      Redis Streams (XADD)        ┌──────────┐
   │ Gateway  │ ───────────────────────────────> │   Ubot   │ ──> [ Telegram MTProto ]
   │ (Bot UI) │ <─────────────────────────────── │ (Engine) │
   └──────────┘      Ack & Progress Events       └──────────┘
        │                                              │
        ▼                                              ▼
   [ SQLite + Mongo ]                            [ SQLite + Mongo ]
```

### Aturan Wajib Penggunaan Redis Streams (Senior Go Standard)
1. **Parallel Worker Pool (Anti Head-of-Line Blocking):**
   - Dilarang memproses pesan stream secara serial di dalam loop pembaca `XReadGroup`.
   - Reader hanya mengambil batch pesan dan menyalurkannya ke **Bounded Worker Pool** (16–32 worker goroutine) melalui buffered job channel.
2. **Pending Entries List (PEL) Recovery & AutoClaim:**
   - Baik di `gateway` maupun `ubot`, listener wajib menangani recovery pesan tertunda saat startup (`ID: "0"`).
   - Terapkan `XAutoClaim` untuk mengambil alih tugas yang tertinggal jika salah satu worker mengalami crash/restart mendadak (Zero Message Loss).
3. **Idempotency Guard:**
   - Konsumsi pesan bersifat *At-Least-Once*. Operasi kritikal (klaim token, eksekusi broadcast) wajib dipagari `SetNX` idempotency key berbasis `TaskID` agar terhindar dari eksekusi ganda saat network retries.
4. **Dead-Letter Queue (DLQ):**
   - Pesan yang gagal diproses berulang kali (setelah 3-5x percobaan) harus di-ACK dari stream utama dan dialihkan ke `stream:dlq:*` untuk mencegah antrean macet oleh payload corrupt (Poison Pill).
5. **Stream Naming Convention:**
   - `stream:cmd:<domain>`: Perintah dari Gateway ke Ubot (contoh: `stream:cmd:auth`, `stream:cmd:broadcast`).
   - `stream:evt:<domain>`: Notifikasi event dari Ubot ke Gateway (contoh: `stream:evt:broadcast:progress`, `stream:evt:session:status`).
   - `stream:dlq:<domain>`: Dead-letter queue untuk investigasi failure.

--------------------------------------------------------------------------------

## 8. UI / Presentation Rules

### All in `ui/` package

The `ui/` package (gateway: `internal/ui/`) contains:
- ALL HTML text templates (message body)
- ALL ReplyKeyboardMarkup definitions
- ALL InlineKeyboardMarkup definitions
- Pure functions only — NO state, NO DB access, NO side effects

### ReplyKeyboard — NEVER MODIFY

> **CRITICAL:** All ReplyKeyboard button text labels (e.g., `"💎 Status 💎"`,
> `"🛒 Shop 🛒"`) MUST be preserved exactly as-is. These are part of the
> user navigation contract.

### Text Template Pattern

```go
// ui/text.go — pure function, receives pre-formatted data
func StatusDashboard(rows []StatusRow, lastAction, lastEvent string) string {
    var b strings.Builder
    b.WriteString("<b>📊 Status Userbot</b>\n")
    b.WriteString("━━━━━━━━━━━━━━━━━━━━\n")
    for _, r := range rows {
        fmt.Fprintf(&b, "<b>%s:</b> %s\n", r.Label, r.Value)
    }
    return b.String()
}
```

--------------------------------------------------------------------------------

## 9. Naming Conventions (Go Code)

| Element | Convention | Example |
|---------|-----------|---------|
| Package | lowercase, single word | `panel`, `store`, `broker` |
| Interface | `-er` suffix or descriptive | `ConfigReader`, `BotSender`, `Publisher` |
| Struct | PascalCase noun | `Service`, `State`, `Handler` |
| Constructor | `New` + type name | `NewService(...)`, `NewState()` |
| Method | verb + object | `ShowStatus`, `GetConfig`, `DeleteGroup` |
| File | lowercase, underscore for multi-word | `reply_keyboard.go`, `inline_keyboard.go` |
| Test file | `*_test.go` alongside source | `service_test.go` |
| Const group | `CamelCase` with prefix | `BtnStatus`, `IkbRefreshStatus` |
| Callback data | `domain:action:param` | `"panel:list:delete:123"` |

--------------------------------------------------------------------------------

## 10. Import Rules

### Standard grouping (enforced by `goimports`)

```go
import (
    // 1. Standard library
    "context"
    "fmt"

    // 2. Internal packages (same module)
    "assterisk/gateway/internal/panel"
    "assterisk/gateway/internal/ui"

    // 3. External dependencies
    "github.com/gotd/td/tg"
    "github.com/redis/go-redis/v9"
)
```

### Forbidden imports

| From | Cannot import | Reason |
|------|--------------|--------|
| Domain (`panel/`) | Another domain (`token/`) | Domains are independent; communicate via service interfaces |
| Domain (`panel/`) | `infra/store` directly | Use interface defined in domain; infra implements it |
| `ui/` | Any domain or infra | UI is pure presentation, no dependencies |
| `infra/store` | Any domain | Store is lower layer; domains depend on store, not reverse |

### Allowed dependency direction

```
cmd/ → app/ → domain packages → (interfaces only)
                    ↓
              infra/ (implements domain interfaces)
              ui/   (called by domain services)
```

--------------------------------------------------------------------------------

## 11. Testing Standards

| Rule | Detail |
|------|--------|
| **Test file location** | Same package, `*_test.go` |
| **External test package** | Use `package panel_test` for black-box tests |
| **Mock interfaces** | Define mock in test file or `mock_test.go` |
| **Table-driven tests** | Preferred for functions with multiple input/output combinations |
| **No real infra in unit tests** | Use mock/stub for DB, Redis, Telegram |
| **Build check** | `go build ./...` must pass with zero errors |
| **Test check** | `go test ./...` must pass |

--------------------------------------------------------------------------------

## 12. Migration Checklist

When refactoring existing code to comply with this architecture:

1. [ ] Create `cmd/<service>/main.go` — move entrypoint from `main/main.go`
2. [ ] Rename `applications/` → `internal/`
3. [ ] Split `controllers/` into domain packages under `internal/`
4. [ ] Move `assets/` → `internal/ui/`
5. [ ] Move `botgotd/` → `internal/infra/telegram/`
6. [ ] Move `broker/` → `internal/infra/broker/`
7. [ ] Move `databases/` → `internal/infra/store/`
8. [ ] Move `config/` → `internal/infra/config/`
9. [ ] Dissolve `models/` — move types to domain or `shared/`
10. [ ] Dissolve `utils/` — move functions to domain packages
11. [ ] Split god files (>400 LOC) into sub-concern files
12. [ ] Extract `state.go` from any file with mutex+map patterns
13. [ ] Define interfaces at consumer side for cross-package deps
14. [ ] Update all `import` paths
15. [ ] Run `go build ./...` — zero errors
16. [ ] Run `go test ./...` — all pass
17. [ ] Verify no file exceeds 400 LOC

### Things that MUST NOT change during refactor

- ReplyKeyboard button text labels (exact strings preserved)
- Callback data strings (`"panel:list:delete:{id}"`, `"assist_help:{section}"`, etc.)
- Redis Stream channel names and payload structure
- Database schema and query logic (move, don't rewrite)
- `go.mod` module paths (update import paths, not module name — unless renaming module)
- Telegram bot behavior (same messages, same buttons, same flows)
