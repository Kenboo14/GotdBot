# AI_GUIDE.md — GotdBot Engineering & Architecture Guide

> **Purpose:** Permanent engineering instructions and official architectural roadmap for AI agents and engineers working on GotdBot.
>
> **Core Target:** Run **500 persistent MTProto Telegram accounts 24/7** reliably on a constrained VPS (**2 GB RAM, 1 vCPU**) without OOM crashes or CPU starvation.
>
> **Architectural Solution:** **Hybrid Storage Pattern** (Local Pure-Go SQLite as Primary Store + Cloud MongoDB Atlas as Asynchronous Backup) + Persistent MTProto 24/7 + Staggered Lifecycle Startup + Strict Docker Resource Constraints.

---

# 1. Mandatory Instructions for AI Agents

Before reading or modifying any code:

1. Read this `AI_GUIDE.md` completely.
2. Read `GRAVITY_ANALIS.md` to understand the full capacity planning, resource formulas, and audit findings.
3. Read `CODEX_HANDOFF.md` for lifecycle tokens and mutex ownership rules.
4. Inspect actual implementation before assuming behavior.
5. Create a concise implementation plan before modifying any logic.
6. Prefer the smallest, safest change that directly satisfies the requirement.

## 1.1 Non-Negotiable Rules

- **Never reintroduce idle-based MTProto disconnection.** Connection stays alive 24/7 as long as authorization is valid.
- **Never evict an active Telegram session to make room for another account.**
- **Never run local MongoDB 8 container without strict WiredTiger cache limits** (or better: replace with SQLite primary).
- **Never perform simultaneous mass-connect of 500 accounts on startup.** Always use staggered startup (jitter) to protect 1 vCPU.
- **Never use CGO-dependent SQLite libraries.** Use pure-Go SQLite (`modernc.org/sqlite` or `github.com/glebarez/go-sqlite`) to maintain Alpine `CGO_ENABLED=0` compatibility.
- **Never perform synchronous cloud MongoDB writes in the MTProto message path.** Backup syncing must be asynchronous / non-blocking.
- **Never let broadcast cancellation terminate the account's MTProto lifecycle.**
- **Never let an old account lifecycle delete the state of a newer lifecycle** (enforce Generation IDs).
- **Never claim performance or race-safety improvements without benchmark / pprof measurements.**
- **Never expose internal pprof (127.0.0.1:6060) to the public internet.**
- **Never leak Telegram sessions, bot tokens, API hashes, or encryption keys in logs or git.**

---

# 2. System Architecture & Topology

GotdBot is an asynchronous event-driven multi-service system:

```
[ Telegram Users / Bot API ]
             │
             ▼
    ┌─────────────────┐
    │  gateway (Go)   │  (Telegram Bot API Poller & Onboarding Flow)
    └────────┬────────┘
             │ (Redis Streams: to:ubot:auth, to:ubot:command, to:ubot:broadcast)
             ▼
      [ Redis 7 Alpine ]
             ▲
             │ (Redis Streams: from:ubot)
    ┌────────┴────────┐
    │    ubot (Go)    │  (Core MTProto Engine, Dispatcher, Workers, pprof)
    └────────┬────────┘
             │
      ┌──────┴─────────────────────────────────┐
      │ (Local Primary - Fast, 15MB RAM)       │ (Async Offsite Backup - 0MB Host RAM)
      ▼                                        ▼
┌──────────────┐                       ┌──────────────┐
│ SQLite (WAL) │                       │ Mongo Atlas  │
│ userbot.db   │                       │ (Free Cloud) │
└──────────────┘                       └──────────────┘
```

### Module Responsibilities:
1. **`gateway` (Edge Bot Gateway)**:
   - Long-polling Telegram Bot API (`getUpdates`).
   - Handles onboarding conversation (`/create`, phone number, OTP, 2FA password).
   - Rate limiting and dispatching jobs to Redis Streams.
2. **`ubot` (Core MTProto Engine)**:
   - Persistent MTProto connections via `github.com/gotd/td`.
   - In-memory dispatching for userbot commands (`!p`, `!gcast`, `!help`, etc.).
   - Primary state storage on local SQLite with asynchronous remote replication to MongoDB.
   - Internal diagnostic server via pprof on `127.0.0.1:6060`.
3. **`redis` (Event Broker)**:
   - Decouples `gateway` and `ubot`.
   - Approximate stream trimming (`MaxLen: 1000`) prevents memory bloat.

---

# 3. Target Capacity & RAM Budget (2 GB VPS)

To host **500 persistent MTProto accounts** on **2 GB RAM (2,048 MB) and 1 vCPU**, memory allocation must adhere strictly to this budget:

| Process / Component | Memory Target | Enforcement Mechanism |
| :--- | :--- | :--- |
| **Linux OS & Dockerd** | ~150 MB | Base system footprint |
| **Redis 7 Alpine** | ~40 MB | Docker `mem_limit: 80m`, Stream `MaxLen: 1000` |
| **Gateway Service** | ~40 MB | Docker `mem_limit: 80m` |
| **Ubot Base Runtime** | ~80 MB | Go runtime base, Redis client, pprof |
| **500 MTProto Clients** | ~1,000 MB (~2 MB/client) | Idle dispatcher, socket buffers, `GOMEMLIMIT=1100MiB` |
| **Primary SQLite DB** | ~15 MB | In-process pure-Go SQLite with WAL mode |
| **Total Process Footprint** | **~1,325 MB** | **Safe headroom of ~720 MB on 2 GB host** |
| **Linux Host Swap** | **2,048 MB** | Dedicated swap file prevents OOM killer during spikes |

*(Note: If MongoDB container is kept locally, it steals 400-600 MB, reducing free margin to <150 MB and risking immediate host OOM).*

---

# 4. Storage Architecture: SQLite Primary + Cloud Mongo Backup

### 4.1 Why SQLite as Primary?
- **Data Size**: 500 accounts represent only ~3-5 MB total storage.
- **Resource Footprint**: Consumes only ~15 MB RAM vs. 500-700 MB for a standalone MongoDB container.
- **Latency**: In-process disk/WAL operations take <1 ms, zero TCP network latency.
- **Portability**: The entire state resides in a single volume-mounted file: `/data/userbot.db`.

### 4.2 Pure-Go SQLite Driver Requirement
To maintain compatibility with non-CGO Alpine Docker builds (`CGO_ENABLED=0`), GotdBot must use pure-Go SQLite:
- `modernc.org/sqlite` OR `github.com/glebarez/go-sqlite`
- **DO NOT** use `github.com/mattn/go-sqlite3` (requires gcc and CGO).

### 4.3 SQLite Schema Design
```sql
-- Sessions table
CREATE TABLE IF NOT EXISTS sessions (
    chat_id INTEGER PRIMARY KEY,
    owner_id INTEGER NOT NULL,
    account_id INTEGER NOT NULL,
    session_blob TEXT NOT NULL,
    prefix TEXT DEFAULT '!',
    prefix_set INTEGER DEFAULT 0,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sessions_account ON sessions(account_id);
CREATE INDEX IF NOT EXISTS idx_sessions_owner ON sessions(owner_id);

-- Sudo table
CREATE TABLE IF NOT EXISTS sudo_users (
    owner_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (owner_id, user_id)
);

-- Blacklist table
CREATE TABLE IF NOT EXISTS broadcast_blacklist (
    owner_id INTEGER NOT NULL,
    chat_type TEXT NOT NULL,
    chat_id INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (owner_id, chat_type, chat_id)
);
```

### 4.4 WAL Mode & Concurrency
SQLite must always be initialized with:
```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
```

### 4.5 Asynchronous Cloud MongoDB Backup
1. **Target**: MongoDB Atlas Free Tier (M0 - 512 MB free cloud database) or remote MongoDB instance.
2. **Strategy**:
   - On new user registration (`/create` -> `authorize`): write to SQLite immediately, then queue an asynchronous write to MongoDB in background goroutine.
   - Periodic Snapshot: A background cron/ticker in `ubot` verifies and syncs dirty records every 6 hours.
   - **Disaster Recovery**: If `userbot.db` is missing on boot, `ubot` connects to MongoDB Atlas, restores all records into SQLite, and proceeds with normal startup.

---

# 5. Persistent MTProto Lifecycle & Startup Jitter

### 5.1 Persistent Connection Policy
- One account = One MTProto Client (`telegram.Client`).
- Remains connected 24/7 as long as session is valid and host is running.
- No idle disconnect. No eviction of active accounts.

### 5.2 Staggered Startup (Anti-CPU-Starvation)
When starting up or restarting with 500 accounts:
- **Problem**: 500 simultaneous MTProto cryptographic handshakes (Diffie-Hellman + TLS) will consume 100% of 1 vCPU, timing out connections and causing restart loops.
- **Solution (Mandatory)**: In `Restore()`, enforce a jitter/delay:
  ```go
  for cur.Next(ctx) {
      // Decode row...
      s.activate(row.ChatID, row.AccountID)
      time.Sleep(30 * time.Millisecond) // 500 accounts activate smoothly over ~15 seconds
  }
  ```

### 5.3 Generation-Safe Lifecycle Tokens
Every activation increments `s.nextGen` and stores a unique token `genID`:
- `s.connecting[chatID] = genID`
- `s.accountGens[chatID] = genID`
- Defer cleanup blocks verify ownership:
  ```go
  if s.accountGens[chatID] == genID {
      delete(s.activeCancels, chatID)
      delete(s.accountCtxs, chatID)
      delete(s.accountGens, chatID)
  }
  ```
An older dying lifecycle can never delete the active state of a newer lifecycle.

### 5.4 Admission Limit
- Set `MAX_ACTIVE_SESSIONS=550` in `.env` to accommodate the 500 target + buffer.
- Atomic reservation in `activate()`:
  ```go
  if len(s.active) + len(s.connecting) >= maxActiveSessions {
      log.Printf("[PoolCap] Maximum capacity reached (%d). Rejecting chatID=%d", maxActiveSessions, chatID)
      return
  }
  ```

---

# 6. Broadcast (`!gcast`) Engineering & Safety

### 6.1 Concurrency & Throughput
- Global concurrency limit: `broadcastSem = make(chan struct{}, 5)`. Maximum 5 concurrent broadcasts system-wide.
- Per-message delay: `350ms` (`time.Sleep(350 * time.Millisecond)`).
- Max throughput: 5 jobs × (1 msg / 0.35s) = **~14.2 msgs/sec**.

### 6.2 FloodWait Handling (Mandatory)
If Telegram returns `FLOOD_WAIT_X`:
```go
if flood, ok := tgerr.AsFloodWait(sendErr); ok {
    waitDur := time.Duration(flood.Value)*time.Second + time.Second
    select {
    case <-ctx.Done():
        return
    case <-time.After(waitDur):
        // Retry current target or log backoff
    }
}
```
Never aggressively pound Telegram endpoints when rate-limited.

---

# 7. Sudo & Blacklist Cache Management

- `sudoCache` stores `"ownerID:userID"` permissions with a 30-minute TTL.
- Bounded capacity: Max 2,000 entries.
- Reaper runs every 2 minutes:
  1. Purges expired entries (`time.Now().After(entry.expiresAt)`).
  2. If size still > 2,000, gradually prunes oldest 20% (400 entries) to avoid cache stampede.
- Operational errors (network/disk failure) must **fail-closed** without caching the negative decision.

---

# 8. Implementation Roadmap & Multi-Stage Workflow

To execute the solution cleanly, follow this exact 5-stage workflow:

```
[ Stage 1: VPS Host Prep ]
  ├── 2 GB Linux Swapfile creation
  ├── Docker Compose mem_limit & environment configuration
  └── Verify pprof local binding (127.0.0.1:6060)
         │
         ▼
[ Stage 2: Pure-Go SQLite Implementation ]
  ├── Add modernc.org/sqlite dependency (no CGO)
  ├── Create database/sqlite.go with WAL mode
  ├── Create storage adapter interface
  └── Implement session, sudo, and blacklist SQLite repositories
         │
         ▼
[ Stage 3: Cloud MongoDB Async Backup Client ]
  ├── Refactor MongoDB client as an offsite backup consumer
  ├── Implement async replicate on login/update
  └── Implement auto-restore on boot if SQLite DB is empty
         │
         ▼
[ Stage 4: Lifecycle & Broadcast Tuning ]
  ├── Add 30ms staggered jitter in Restore() loop
  ├── Add FloodWait backoff handling in command_broadcast.go
  └── Clean up dead code (common/proto, unused bot middleware)
         │
         ▼
[ Stage 5: Staged Benchmark & Validation ]
  ├── Phase 1: 10 accounts (baseline heap & goroutine audit)
  ├── Phase 2: 100 accounts (verify admission limit & SQLite speed)
  ├── Phase 3: 250 accounts (verify CPU jitter & stream health)
  ├── Phase 4: 500 accounts (24-hour endurance test & pprof audit)
  └── Phase 5: Concurrency test (5 simultaneous broadcasts at 500 accounts)
```

---

# 9. Docker Deployment Configuration

Official production `docker-compose.yaml` specification for the 2 GB VPS:

```yaml
services:
  redis:
    image: redis:7-alpine
    command: redis-server --appendonly yes --requirepass ${REDIS_PASS:-change-me-for-local-dev}
    mem_limit: 80m
    volumes:
      - redis_data:/data
    healthcheck:
      test: ["CMD-SHELL", "redis-cli -a $$REDIS_PASS ping"]
      interval: 5s
      timeout: 3s
      retries: 10

  gateway:
    image: ghcr.io/kenboo14/gotdbot-gateway:latest
    mem_limit: 80m
    environment:
      BOT_TOKEN: ${BOT_TOKEN:?Set BOT_TOKEN}
      REDIS_ADDR: redis:6379
      REDIS_PASS: ${REDIS_PASS:-change-me-for-local-dev}
    depends_on:
      redis:
        condition: service_healthy

  ubot:
    image: ghcr.io/kenboo14/gotdbot-ubot:latest
    mem_limit: 1250m
    environment:
      BOT_TOKEN: ${BOT_TOKEN:?Set BOT_TOKEN}
      REDIS_ADDR: redis:6379
      REDIS_PASS: ${REDIS_PASS:-change-me-for-local-dev}
      API_ID: ${API_ID:?Set API_ID}
      API_HASH: ${API_HASH:?Set API_HASH}
      SESSION_ENCRYPTION_KEY: ${SESSION_ENCRYPTION_KEY:?Set SESSION_ENCRYPTION_KEY}
      MAX_ACTIVE_SESSIONS: 550
      GOMEMLIMIT: 1100MiB
      GOGC: 50
      SQLITE_PATH: /app/data/userbot.db
      MONGO_BACKUP_URI: ${MONGO_BACKUP_URI:-}
      MONGO_BACKUP_DB: ${MONGO_BACKUP_DB:-gotdbot_backup}
    volumes:
      - ubot_data:/app/data
    depends_on:
      redis:
        condition: service_healthy

volumes:
  redis_data:
  ubot_data:
```

*(Notice: The local `mongo` container is completely removed from the host composition, saving ~500 MB RAM immediately!)*

---

# 10. Diagnostics, Verification & Testing

### 10.1 Profiling with pprof
Access internal metrics locally on the VPS:
```bash
# Heap memory profile
curl http://127.0.0.1:6060/debug/pprof/heap > heap.pprof
go tool pprof -top heap.pprof

# Goroutine dump
curl http://127.0.0.1:6060/debug/pprof/goroutine > goroutine.pprof
go tool pprof -top goroutine.pprof
```

### 10.2 Essential Validation Suite
Every code change must pass:
```bash
cd ubot
go vet ./...
go test -v -count=1 ./applications/modules/userbot/...
cd ../gateway
go vet ./...
```

### 10.3 Definition of Done (DoD) for 500-Account Readiness
1. Pure-Go SQLite operational as primary storage with zero CGO dependency.
2. Local MongoDB container eliminated from `docker-compose.yaml`.
3. 2 GB swapfile confirmed active on VPS host (`swapon --show`).
4. Staggered startup jitter (30ms) verified during `Restore()`.
5. 500 simulated/real accounts connect without exceeding 1.25 GB RSS on `ubot`.
6. Host free memory remains ≥ 300 MB during idle 24/7 operation.
7. 5 concurrent broadcasts execute with CPU utilization stabilized below 90%.
8. Zero data loss during container restart (persisted in SQLite volume).

---
*End of AI_GUIDE.md — Authoritative Engineering Reference.*
