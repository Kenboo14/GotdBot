package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"assterisk/gateway/internal/botgotd"
)

type simulasiState struct {
	mu            sync.Mutex
	active        bool
	stres         int
	idle          int
	aktif         int
	updatedAt     int64
	memBlocks     [][]byte
	cancelFunc    func()
	cpuCounter    int64
}

var globalSimulasi = &simulasiState{}

func (s *simulasiState) publishRedis(rdb *redis.Client) {
	if rdb == nil {
		return
	}
	s.mu.Lock()
	state := "inactive"
	if s.active {
		state = "active"
	}
	payload := map[string]any{
		"active":     s.active,
		"state":      state,
		"stres":      s.stres,
		"idle":       s.idle,
		"aktif":      s.aktif,
		"updated_at": s.updatedAt,
	}
	s.mu.Unlock()

	jsonData, _ := json.Marshal(payload)
	_ = rdb.Set(context.Background(), "simulasi:state", jsonData, 0).Err()
}

func (s *simulasiState) getSnapshot() (active bool, stres, idle, aktif int, updatedAt int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active, s.stres, s.idle, s.aktif, s.updatedAt
}

func (h *Handler) SimulasiCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.Message == nil {
		return
	}

	// Only OWNER can run simulasi
	callerID := update.Message.From.ID
	if callerID == 0 || callerID != h.getOwnerID() {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "<b>❌ Akses Ditolak</b>\nPerintah /simulasi hanya bisa digunakan oleh OWNER.",
		})
		return
	}

	args := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, "/simulasi"))
	fields := strings.Fields(args)

	if len(fields) >= 1 && fields[0] == "stop" {
		globalSimulasi.mu.Lock()
		if !globalSimulasi.active {
			globalSimulasi.mu.Unlock()
			b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "<b>ℹ️ Simulasi tidak sedang aktif.</b>",
			})
			return
		}
		if globalSimulasi.cancelFunc != nil {
			globalSimulasi.cancelFunc()
			globalSimulasi.cancelFunc = nil
		}
		globalSimulasi.memBlocks = nil
		globalSimulasi.active = false
		globalSimulasi.stres = 0
		globalSimulasi.idle = 0
		globalSimulasi.aktif = 0
		globalSimulasi.updatedAt = time.Now().Unix()
		globalSimulasi.mu.Unlock()

		globalSimulasi.publishRedis(h.broker.Client)

		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "<b>🛑 Simulasi dihentikan.</b> Semua resource simulasi telah dibuang.",
		})
		return
	}

	var stres, idle, aktif int
	if len(fields) >= 1 {
		fmt.Sscanf(fields[0], "%d", &stres)
	}
	if len(fields) >= 2 {
		fmt.Sscanf(fields[1], "%d", &idle)
	}
	if len(fields) >= 2 {
		fmt.Sscanf(fields[2], "%d", &aktif)
	}
	total := stres + idle + aktif

	if total == 0 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "<b>ℹ️ Penggunaan:</b>\n<code>/simulasi <stres> <idle> <aktif></code>\nContoh: <code>/simulasi 200 300 500</code>\nUntuk menghentikan: <code>/simulasi stop</code>",
		})
		return
	}

	if total > 2000 {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "<b>⚠️ Total terlalu besar.</b> Maksimal 2000 sesi simulasi untuk mencegah OOM.",
		})
		return
	}

	// Stop old simulasi if running
	globalSimulasi.mu.Lock()
	if globalSimulasi.active && globalSimulasi.cancelFunc != nil {
		globalSimulasi.cancelFunc()
		globalSimulasi.cancelFunc = nil
	}
	globalSimulasi.memBlocks = nil

	// Allokasi RAM nyata
	memBlocks := make([][]byte, 0, total)
	for i := 0; i < stres; i++ {
		block := make([]byte, 2*1024*1024)
		for j := range block {
			block[j] = byte(j)
		}
		memBlocks = append(memBlocks, block)
	}
	for i := 0; i < idle; i++ {
		memBlocks = append(memBlocks, make([]byte, 1*1024*1024))
	}
	for i := 0; i < aktif; i++ {
		memBlocks = append(memBlocks, make([]byte, 3*1024*1024))
	}

	globalSimulasi.memBlocks = memBlocks
	globalSimulasi.active = true
	globalSimulasi.stres = stres
	globalSimulasi.idle = idle
	globalSimulasi.aktif = aktif
	globalSimulasi.updatedAt = time.Now().Unix()

	// Spawn CPU goroutines
	simCtx, simCancel := context.WithCancel(context.Background())
	globalSimulasi.cancelFunc = simCancel
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
					time.Sleep(time.Microsecond * 100)
				}
			}
		}()
	}
	globalSimulasi.mu.Unlock()

	globalSimulasi.publishRedis(h.broker.Client)

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

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:    update.Message.Chat.ID,
		Text:      report,
		ParseMode: botgotd.ParseModeHTML,
	})
}

func (h *Handler) getOwnerID() int64 {
	ownerStr := strings.TrimSpace(os.Getenv("OWNER"))
	if ownerStr == "" {
		ownerStr = strings.TrimSpace(os.Getenv("OWNER_ID"))
	}
	var id int64
	fmt.Sscanf(ownerStr, "%d", &id)
	return id
}
