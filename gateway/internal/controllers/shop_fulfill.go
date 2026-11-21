package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (h *Handler) StartShopServices(ctx context.Context, b *botgotd.Bot) {
	cfg := loadPGConfig()
	listenAddr := cfg.CallbackListenAddr
	if listenAddr == "" && resolveInternalAdminKey() != "" {
		listenAddr = ":8085"
	}
	if listenAddr != "" {
		go h.runShopCallbackHTTP(ctx, b, listenAddr, cfg.CallbackSecret)
	}
	if cfg.isConfigured() {
		go h.runShopPendingPoller(ctx, b)
	}
}

func (h *Handler) runShopCallbackHTTP(ctx context.Context, b *botgotd.Bot, addr, secret string) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/shop/payment-callback", func(w http.ResponseWriter, r *http.Request) {
		h.serveShopPaymentCallback(w, r, b, secret)
	})
	h.registerAdminHTTPRoutes(mux, b)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()

	log.Printf("[ShopCallback] listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("[ShopCallback] stopped: %v", err)
	}
}

type shopPaidWebhookBody struct {
	InvoiceID     string `json:"invoice_id"`
	ExternalID    string `json:"external_id"`
	BuyerRef      string `json:"buyer_ref"`
	PackageID     string `json:"package_id"`
	Status        string `json:"status"`
	BaseAmount    int64  `json:"base_amount"`
	UniqueCode    int    `json:"unique_code"`
	TotalAmount   int64  `json:"total_amount"`
	PaidAt        int64  `json:"paid_at"`
	SourcePackage string `json:"source_package"`
}

func (h *Handler) serveShopPaymentCallback(w http.ResponseWriter, r *http.Request, b *botgotd.Bot, secret string) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	defer r.Body.Close()
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}

	ts := r.Header.Get("X-PG-Timestamp")
	nonce := r.Header.Get("X-PG-Nonce")
	sigHex := r.Header.Get("X-PG-Signature")

	if !verifyShopHMAC(secret, ts+"."+nonce+"."+string(rawBody), sigHex) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	if !h.shop.checkCallbackReplay(ts, nonce, time.Now()) {
		http.Error(w, "replay rejected", http.StatusUnauthorized)
		return
	}

	var payload shopPaidWebhookBody
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if strings.ToUpper(payload.Status) == "PAID" {
		buyerID, _ := strconv.ParseInt(strings.TrimSpace(payload.BuyerRef), 10, 64)
		inv := pgInvoice{
			ID:          payload.InvoiceID,
			ExternalID:  payload.ExternalID,
			BuyerRef:    payload.BuyerRef,
			PackageID:   payload.PackageID,
			BaseAmount:  payload.BaseAmount,
			UniqueCode:  payload.UniqueCode,
			TotalAmount: payload.TotalAmount,
			Status:      payload.Status,
			PaidAt:      payload.PaidAt,
		}
		h.fulfillInvoiceByData(r.Context(), b, inv, buyerID, buyerID, payload.SourcePackage)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func verifyShopHMAC(secret, message, sigHex string) bool {
	if secret == "" || sigHex == "" {
		return false
	}
	sigBytes, err := hex.DecodeString(strings.TrimSpace(sigHex))
	if err != nil || len(sigBytes) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	return hmac.Equal(sigBytes, mac.Sum(nil))
}

func (h *Handler) runShopPendingPoller(ctx context.Context, b *botgotd.Bot) {
	ticker := time.NewTicker(6 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			pending := h.shop.snapshotPending(now)
			if len(pending) == 0 {
				continue
			}
			cfg := loadPGConfig()
			if !cfg.isConfigured() {
				continue
			}
			for _, ord := range pending {
				inv, err := getPGInvoice(ctx, cfg, ord.InvoiceID)
				if err != nil {
					continue
				}
				if inv.Status == "PAID" {
					h.fulfillInvoiceByData(ctx, b, inv, ord.ChatID, ord.UserID, "")
				} else if inv.Status == "EXPIRED" || inv.Status == "CANCELLED" {
					h.shop.removePending(inv.ID)
				}
			}
		}
	}
}

func (h *Handler) fulfillInvoiceByData(ctx context.Context, b *botgotd.Bot, inv pgInvoice, chatID, buyerID int64, sourcePkg string) {
	ord := h.shop.getPending(inv.ID)
	if !h.shop.claimFulfillOnce(inv.ID) {
		return
	}
	if ord != nil {
		if chatID == 0 {
			chatID = ord.ChatID
		}
		if buyerID == 0 {
			buyerID = ord.UserID
		}
		if inv.PackageID == "" {
			inv.PackageID = ord.PackageID
		}
	}
	if buyerID == 0 {
		buyerID, _ = strconv.ParseInt(strings.TrimSpace(inv.BuyerRef), 10, 64)
	}
	if chatID == 0 {
		chatID = buyerID
	}
	if buyerID <= 0 || h.db == nil {
		return
	}

	pkg, ok := h.resolveShopPackage(inv.PackageID, 0)
	if !ok {
		pkg = shopPackage{
			ID:           inv.PackageID,
			BaseID:       "pro",
			Name:         "💎 Pro",
			Tier:         "Pro",
			Qty:          1,
			DurationDays: 30,
			PriceIDR:     inv.BaseAmount,
		}
	}
	pkgLabel := fmt.Sprintf("%s (%dx • %d Hari)", pkg.Name, pkg.Qty, pkg.DurationDays)
	dur := time.Duration(pkg.DurationDays) * 24 * time.Hour
	if dur <= 0 {
		dur = 30 * 24 * time.Hour
	}

	now := time.Now()
	normTier := databases.NormalizeTier(pkg.Tier)
	curLic, _ := h.db.GetActiveUserLicense(ctx, buyerID)

	var voucherCode string
	var activeLic *databases.UserLicense

	if curLic != nil && curLic.Status == "active" && databases.NormalizeTier(curLic.Tier) == normTier && curLic.ExpiresAt.After(now) {
		cp := *curLic
		cp.ExpiresAt = cp.ExpiresAt.Add(dur)
		_ = h.db.SaveUserLicense(ctx, &cp)
		cfg := h.db.GetUserConfig(ctx, buyerID)
		cfg.MasaAktif = formatLicenseMasaAktif(&cp)
		_ = h.db.SaveUserConfig(ctx, cfg)
		voucherCode = cp.MasterToken
		activeLic = &cp
	} else {
		masterToken := GenerateMasterToken()
		tm := &databases.TokenMaster{
			Token:           masterToken,
			Tier:            normTier,
			DurationSeconds: int64(dur.Seconds()),
			IsShared:        false,
			ClaimCount:      1,
			CreatedBy:       buyerID,
			CreatedAt:       now,
			Status:          "expired",
		}
		_ = h.db.SaveTokenMaster(ctx, tm)

		newLic := &databases.UserLicense{
			TokenSHA:           ComputeTokenSHA(masterToken, buyerID),
			MasterToken:        masterToken,
			OwnerChatID:        buyerID,
			UserbotID:          buyerID,
			Tier:               normTier,
			CanCustomWatermark: databases.GetTierPolicy(normTier).AllowCustomWatermark,
			ExpiresAt:          now.Add(dur),
			CreatedAt:          now,
			Status:             "active",
		}
		_ = h.db.SaveUserLicense(ctx, newLic)
		_ = h.switchUserActiveToken(ctx, buyerID, newLic)
		voucherCode = masterToken
		activeLic = newLic
	}

	if ord != nil && ord.QRMsgID > 0 && b != nil {
		_, _ = b.DeleteMessage(ctx, &botgotd.DeleteMessageParams{ChatID: chatID, MessageID: ord.QRMsgID})
	}

	expStr := activeLic.ExpiresAt.Local().Format("02-01-2006 15:04:05 WIB")
	if b != nil && chatID != 0 {
		msgText := fmt.Sprintf(`<h1>✅ Pembayaran Berhasil</h1>
<table border="1">
  <tr><th>📦 Paket</th><td><b>%s</b></td></tr>
  <tr><th>💰 Nominal</th><td><code>%s</code></td></tr>
  <tr><th>🎖️ Tier Aktif</th><td><code>%s</code></td></tr>
  <tr><th>⏰ Aktif Hingga</th><td><code>%s</code></td></tr>
  <tr><th>🔑 Kode Voucher</th><td><code>%s</code></td></tr>
</table>
<blockquote expandable>Lisensi paket Anda telah aktif otomatis pada akun ini. Simpan kode voucher di atas sebagai arsip pribadi.</blockquote>
<hr/>
<p>GotdBot • Pembayaran Otomatis</p>`,
			html.EscapeString(pkgLabel),
			html.EscapeString(formatRupiah(inv.TotalAmount)),
			html.EscapeString(activeLic.Tier),
			expStr,
			html.EscapeString(voucherCode),
		)
		_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:    chatID,
			Text:      msgText,
			ParseMode: "HTML",
		})
	}

	globalDevMonitor.NotifyInvoice(fmt.Sprintf(
		"Buyer <code>%d</code> • Paket <b>%s</b> • <code>%s</code> • Invoice <code>%s</code> • Source <code>%s</code>",
		buyerID,
		html.EscapeString(pkgLabel),
		html.EscapeString(formatRupiah(inv.TotalAmount)),
		html.EscapeString(inv.ID),
		html.EscapeString(sourcePkg),
	))
}

func (h *Handler) ShopCommand(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return
	}
	chatID := update.Message.Chat.ID
	senderID := update.Message.From.ID
	fields := strings.Fields(strings.TrimSpace(update.Message.Text))
	if len(fields) >= 3 && isOwner(senderID) {
		sub := strings.ToLower(fields[1])
		invID := strings.TrimSpace(fields[2])
		cfg := loadPGConfig()
		if sub == "confirm" {
			inv, err := confirmPGInvoiceAdmin(ctx, cfg, invID)
			if err != nil {
				_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:    chatID,
					Text:      fmt.Sprintf(`<h1>❌ Konfirmasi Gagal</h1><p>%s</p>`, html.EscapeString(err.Error())),
					ParseMode: "HTML",
				})
				return
			}
			h.fulfillInvoiceByData(ctx, b, inv, 0, 0, "owner_manual")
			_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:    chatID,
				Text:      fmt.Sprintf(`<h1>✅ Tagihan Dikonfirmasi</h1><p>Invoice <code>%s</code> telah ditandai lunas dan diproses.</p>`, html.EscapeString(inv.ID)),
				ParseMode: "HTML",
			})
			return
		}
		if sub == "status" {
			inv, err := getPGInvoice(ctx, cfg, invID)
			if err != nil {
				_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
					ChatID:    chatID,
					Text:      fmt.Sprintf(`<h1>❌ Cek Status Gagal</h1><p>%s</p>`, html.EscapeString(err.Error())),
					ParseMode: "HTML",
				})
				return
			}
			_, _ = b.SendMessage(ctx, &botgotd.SendMessageParams{
				ChatID:    chatID,
				Text:      fmt.Sprintf(`<h1>📄 Info Tagihan</h1><p>ID: <code>%s</code><br/>Status: <code>%s</code><br/>Total: <code>%s</code></p>`, html.EscapeString(inv.ID), html.EscapeString(inv.Status), html.EscapeString(formatRupiah(inv.TotalAmount))),
				ParseMode: "HTML",
			})
			return
		}
	}
	h.renderShopCatalog(ctx, b, chatID, senderID, 0)
}
