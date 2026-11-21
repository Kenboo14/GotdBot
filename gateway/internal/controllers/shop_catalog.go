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
	Name         string `json:"name"`
	Tier         string `json:"tier"`
	DurationDays int    `json:"duration_days"`
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

func defaultShopPackages() []shopPackage {
	return []shopPackage{
		{ID: "pro_7d", Name: "💎 Pro 7 Hari", Tier: "Pro", DurationDays: 7, PriceIDR: 15000, Description: "Bebas atur jeda & limit grup penuh"},
		{ID: "pro_30d", Name: "💎 Pro 30 Hari", Tier: "Pro", DurationDays: 30, PriceIDR: 45000, Description: "Bebas atur jeda, topik & ekspor penuh"},
		{ID: "proplus_30d", Name: "👑 Pro++ 30 Hari", Tier: "Pro++", DurationDays: 30, PriceIDR: 65000, Description: "Semua fitur Pro + Custom Watermark & Forward"},
		{ID: "vip_30d", Name: "🌟 VIP 30 Hari", Tier: "Vip", DurationDays: 30, PriceIDR: 100000, Description: "Prioritas maksimal & seluruh fitur tanpa batas"},
	}
}

func loadShopPackages() []shopPackage {
	raw := strings.TrimSpace(os.Getenv("SHOP_PACKAGES_JSON"))
	if raw != "" {
		var pkgs []shopPackage
		if json.Unmarshal([]byte(raw), &pkgs) == nil && len(pkgs) > 0 {
			return pkgs
		}
	}
	return defaultShopPackages()
}

func findShopPackage(id string) (shopPackage, bool) {
	for _, p := range loadShopPackages() {
		if p.ID == id {
			return p, true
		}
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
