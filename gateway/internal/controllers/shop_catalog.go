package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type shopPackage struct {
	ID           string `json:"id"`
	BaseID       string `json:"base_id"`
	Name         string `json:"name"`
	Tier         string `json:"tier"`
	Qty          int    `json:"qty"`
	DurationDays int    `json:"duration_days"`
	UnitPriceIDR int64  `json:"unit_price_idr"`
	SubtotalIDR  int64  `json:"subtotal_idr"`
	DiscountPct  int    `json:"discount_pct"`
	DiscountIDR  int64  `json:"discount_idr"`
	PriceIDR     int64  `json:"price_idr"`
	Description  string `json:"description"`
}

type pgClientConfig struct {
	BaseURL            string
	APIKey             string
	AdminKey           string
	CallbackURL        string
	CallbackSecret     string
	CallbackListenAddr string
	MerchantID         string
}

type pgInvoice struct {
	ID          string `json:"id"`
	MerchantID  string `json:"merchant_id"`
	ExternalID  string `json:"external_id"`
	BuyerRef    string `json:"buyer_ref"`
	PackageID   string `json:"package_id"`
	BaseAmount  int64  `json:"base_amount"`
	UniqueCode  int    `json:"unique_code"`
	TotalAmount int64  `json:"total_amount"`
	QRISDynamic string `json:"qris_dynamic"`
	Status      string `json:"status"`
	ExpiresAt   int64  `json:"expires_at"`
	PaidAt      int64  `json:"paid_at"`
}

type pgCreateInvoiceResult struct {
	Invoice     pgInvoice
	QRPNGBase64 string
}

var shopHTTPClient = &http.Client{Timeout: 10 * time.Second}

func loadPGConfig() pgClientConfig {
	listenAddr := strings.TrimSpace(os.Getenv("PG_CALLBACK_LISTEN_ADDR"))
	merchantID := strings.TrimSpace(os.Getenv("PG_MERCHANT_ID"))
	if merchantID == "" {
		merchantID = "default"
	}
	return pgClientConfig{
		BaseURL:            strings.TrimRight(strings.TrimSpace(os.Getenv("PG_BASE_URL")), "/"),
		APIKey:             strings.TrimSpace(os.Getenv("PG_API_KEY")),
		AdminKey:           strings.TrimSpace(os.Getenv("PG_ADMIN_KEY")),
		CallbackURL:        strings.TrimSpace(os.Getenv("PG_CALLBACK_URL")),
		CallbackSecret:     strings.TrimSpace(os.Getenv("PG_CALLBACK_SECRET")),
		CallbackListenAddr: listenAddr,
		MerchantID:         merchantID,
	}
}

func (c pgClientConfig) isConfigured() bool {
	return c.BaseURL != "" && c.APIKey != ""
}

func baseShopCatalogDefs() []shopPackage {
	return []shopPackage{
		{
			ID:           "pro:1",
			BaseID:       "pro",
			Name:         "💎 Pro",
			Tier:         "Pro",
			Qty:          1,
			DurationDays: 30,
			UnitPriceIDR: 45000,
			PriceIDR:     45000,
			Description:  "Bebas atur jeda, topik & ekspor penuh",
		},
		{
			ID:           "proplus:1",
			BaseID:       "proplus",
			Name:         "👑 Pro++",
			Tier:         "Pro++",
			Qty:          1,
			DurationDays: 30,
			UnitPriceIDR: 65000,
			PriceIDR:     65000,
			Description:  "Semua fitur Pro + Custom Watermark & Forward",
		},
	}
}

func normalizeShopBaseID(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "pro", "pro_7d", "pro_30d":
		return "pro", true
	case "proplus", "pro++", "proplus_30d":
		return "proplus", true
	default:
		return "", false
	}
}

func parseShopPackageSpec(spec string) (string, int, bool) {
	raw := strings.TrimSpace(spec)
	if raw == "" {
		return "", 0, false
	}
	qty := 1
	idPart := raw
	if idx := strings.IndexByte(raw, ':'); idx >= 0 {
		idPart = raw[:idx]
		parsedQty, err := strconv.Atoi(strings.TrimSpace(raw[idx+1:]))
		if err != nil {
			return "", 0, false
		}
		qty = parsedQty
	}
	baseID, ok := normalizeShopBaseID(idPart)
	if !ok {
		return "", 0, false
	}
	return baseID, qty, true
}

func computeShopPackage(base shopPackage, pr shopItemPricing, qty int) shopPackage {
	if qty < 1 {
		qty = 1
	}
	if qty > shopMaxQty {
		qty = shopMaxQty
	}
	unit := pr.PriceIDR
	if unit < shopMinPriceIDR {
		unit = base.UnitPriceIDR
	}
	discPct := pr.DiscountPct
	if discPct < 0 {
		discPct = 0
	}
	if discPct > 99 {
		discPct = 99
	}
	subtotal := unit * int64(qty)
	discIDR := (subtotal * int64(discPct)) / 100
	finalPrice := subtotal - discIDR
	if finalPrice < shopMinPriceIDR {
		finalPrice = shopMinPriceIDR
	}
	return shopPackage{
		ID:           fmt.Sprintf("%s:%d", base.BaseID, qty),
		BaseID:       base.BaseID,
		Name:         base.Name,
		Tier:         base.Tier,
		Qty:          qty,
		DurationDays: 30 * qty,
		UnitPriceIDR: unit,
		SubtotalIDR:  subtotal,
		DiscountPct:  discPct,
		DiscountIDR:  discIDR,
		PriceIDR:     finalPrice,
		Description:  base.Description,
	}
}

func (h *Handler) listShopPackages() []shopPackage {
	defs := baseShopCatalogDefs()
	out := make([]shopPackage, 0, len(defs))
	for _, base := range defs {
		var pr shopItemPricing
		if h != nil && h.shop != nil {
			pr = h.shop.getPricing(base.BaseID)
		} else {
			pr = shopItemPricing{PriceIDR: base.UnitPriceIDR, DiscountPct: 0}
		}
		out = append(out, computeShopPackage(base, pr, 1))
	}
	return out
}

func (h *Handler) resolveShopPackage(spec string, qtyOverride int) (shopPackage, bool) {
	baseID, qty, ok := parseShopPackageSpec(spec)
	if !ok {
		return shopPackage{}, false
	}
	if qtyOverride > 0 {
		qty = qtyOverride
	}
	for _, base := range baseShopCatalogDefs() {
		if base.BaseID != baseID {
			continue
		}
		var pr shopItemPricing
		if h != nil && h.shop != nil {
			pr = h.shop.getPricing(base.BaseID)
		} else {
			pr = shopItemPricing{PriceIDR: base.UnitPriceIDR, DiscountPct: 0}
		}
		return computeShopPackage(base, pr, qty), true
	}
	return shopPackage{}, false
}

func formatRupiah(amount int64) string {
	neg := amount < 0
	if neg {
		amount = -amount
	}
	s := strconv.FormatInt(amount, 10)
	if len(s) <= 3 {
		if neg {
			return "-Rp " + s
		}
		return "Rp " + s
	}
	var out []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, byte(c))
	}
	if neg {
		return "-Rp " + string(out)
	}
	return "Rp " + string(out)
}

func createPGInvoice(ctx context.Context, cfg pgClientConfig, extID, buyerRef string, pkg shopPackage) (pgCreateInvoiceResult, error) {
	if !cfg.isConfigured() {
		return pgCreateInvoiceResult{}, fmt.Errorf("pembayaran otomatis belum dikonfigurasi")
	}
	bodyMap := map[string]any{
		"merchant_id": cfg.MerchantID,
		"external_id": extID,
		"buyer_ref":   buyerRef,
		"package_id":  pkg.ID,
		"base_amount": pkg.PriceIDR,
		"include_png": true,
	}
	if cfg.CallbackURL != "" {
		bodyMap["callback_url"] = cfg.CallbackURL
	}
	raw, err := json.Marshal(bodyMap)
	if err != nil {
		return pgCreateInvoiceResult{}, fmt.Errorf("encode request invoice: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cfg.BaseURL+"/v1/invoices", bytes.NewReader(raw))
	if err != nil {
		return pgCreateInvoiceResult{}, fmt.Errorf("build request invoice: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", cfg.APIKey)

	resp, err := shopHTTPClient.Do(req)
	if err != nil {
		return pgCreateInvoiceResult{}, fmt.Errorf("hubungi server pembayaran: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return pgCreateInvoiceResult{}, fmt.Errorf("baca respons invoice: %w", err)
	}
	var parsed struct {
		OK          bool      `json:"ok"`
		Error       string    `json:"error"`
		Invoice     pgInvoice `json:"invoice"`
		QRPNGBase64 string    `json:"qr_png_base64"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return pgCreateInvoiceResult{}, fmt.Errorf("decode respons invoice: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !parsed.OK {
		msg := parsed.Error
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return pgCreateInvoiceResult{}, fmt.Errorf("%s", msg)
	}
	return pgCreateInvoiceResult{
		Invoice:     parsed.Invoice,
		QRPNGBase64: parsed.QRPNGBase64,
	}, nil
}

func doPGInvoiceAction(ctx context.Context, method, fullURL, authHeaderName, authHeaderVal string) (pgInvoice, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, fullURL, nil)
	if err != nil {
		return pgInvoice{}, fmt.Errorf("build pg request: %w", err)
	}
	req.Header.Set(authHeaderName, authHeaderVal)
	resp, err := shopHTTPClient.Do(req)
	if err != nil {
		return pgInvoice{}, fmt.Errorf("request pg server: %w", err)
	}
	defer resp.Body.Close()

	var parsed struct {
		OK      bool      `json:"ok"`
		Error   string    `json:"error"`
		Invoice pgInvoice `json:"invoice"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&parsed); err != nil {
		return pgInvoice{}, fmt.Errorf("decode pg response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !parsed.OK {
		if parsed.Error == "" {
			parsed.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return pgInvoice{}, fmt.Errorf("%s", parsed.Error)
	}
	return parsed.Invoice, nil
}

func getPGInvoice(ctx context.Context, cfg pgClientConfig, invoiceID string) (pgInvoice, error) {
	if !cfg.isConfigured() {
		return pgInvoice{}, fmt.Errorf("pembayaran otomatis belum dikonfigurasi")
	}
	return doPGInvoiceAction(ctx, http.MethodGet, cfg.BaseURL+"/v1/invoices/"+invoiceID, "X-API-Key", cfg.APIKey)
}

func cancelPGInvoice(ctx context.Context, cfg pgClientConfig, invoiceID string) (pgInvoice, error) {
	if !cfg.isConfigured() {
		return pgInvoice{}, fmt.Errorf("pembayaran otomatis belum dikonfigurasi")
	}
	return doPGInvoiceAction(ctx, http.MethodPost, cfg.BaseURL+"/v1/invoices/"+invoiceID+"/cancel", "X-API-Key", cfg.APIKey)
}

func confirmPGInvoiceAdmin(ctx context.Context, cfg pgClientConfig, invoiceID string) (pgInvoice, error) {
	if cfg.BaseURL == "" || cfg.AdminKey == "" {
		return pgInvoice{}, fmt.Errorf("PG_ADMIN_KEY belum dikonfigurasi")
	}
	return doPGInvoiceAction(ctx, http.MethodPost, cfg.BaseURL+"/v1/admin/invoices/"+invoiceID+"/confirm", "X-Admin-Key", cfg.AdminKey)
}
