package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestShopTwoTypesQtyDiscountAndOwnerPricingPersistence(t *testing.T) {
	t.Setenv("OWNER", "9001")
	t.Setenv("PG_API_KEY", "test-api-key")
	t.Setenv("PG_ADMIN_KEY", "test-admin-key")
	t.Setenv("PG_CALLBACK_SECRET", "cb-secret-qty-123")

	var gotPkgID string
	var gotBaseAmount int64
	pngTiny := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n"))
	pgMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/invoices" && r.Method == http.MethodPost {
			var req struct {
				PackageID  string `json:"package_id"`
				BaseAmount int64  `json:"base_amount"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			gotPkgID = req.PackageID
			gotBaseAmount = req.BaseAmount
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":            true,
				"qr_png_base64": pngTiny,
				"invoice": map[string]any{
					"id":           "inv_qty_1",
					"merchant_id":  "default",
					"external_id":  "shop_5001_qty",
					"buyer_ref":    "5001",
					"package_id":   req.PackageID,
					"base_amount":  req.BaseAmount,
					"unique_code":  111,
					"total_amount": req.BaseAmount + 111,
					"status":       "PENDING",
					"expires_at":   time.Now().Add(15 * time.Minute).Unix(),
				},
			})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/admin/merchants/") {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "configured": false})
			return
		}
		http.NotFound(w, r)
	}))
	defer pgMock.Close()
	t.Setenv("PG_BASE_URL", pgMock.URL)

	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "shop_pricing.db")
	dm, err := databases.NewDataManager(context.Background(), context.Background(), "", "", dbPath)
	if err != nil {
		t.Fatalf("NewDataManager failed: %v", err)
	}
	defer func() {
		if sq := dm.GetSQLiteDB(); sq != nil {
			_ = sq.Close()
		}
	}()

	h := NewHandler(nil, dm)
	ctx := context.Background()

	h.ShowShop(ctx, b, 5001)
	reqs := rpcLog.getRequests()
	lastSend := reqs[len(reqs)-1]
	rm, _ := lastSend.Body["reply_markup"].(map[string]any)
	ik, _ := rm["inline_keyboard"].([]any)
	if len(ik) != 1 {
		t.Fatalf("expected 1 row with 2 tier buttons, got %d rows: %+v", len(ik), ik)
	}
	btnRow, _ := ik[0].([]any)
	if len(btnRow) != 2 {
		t.Fatalf("expected strictly 2 tier buttons (Pro & Pro++), got %d: %+v", len(btnRow), btnRow)
	}
	rawShow, _ := json.Marshal(lastSend.Body)
	if strings.Contains(string(rawShow), "vip") || strings.Contains(string(rawShow), "7 Hari") {
		t.Fatalf("catalog should only sell Pro and Pro++, got %s", string(rawShow))
	}

	helpText, _ := h.renderOwnerHelp(ctx, 6)
	for _, want := range []string{`owner_pg:price:pro`, `owner_pg:price:proplus`, `owner_pg:disc:pro`, `owner_pg:disc:all`, `owner_pg:disc:clear`} {
		if !strings.Contains(helpText, want) {
			t.Fatalf("owner /help PG tab missing button %q", want)
		}
	}

	h.OwnerPGCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:      "cb_price_pro",
			From:    botgotd.User{ID: 9001},
			Data:    "owner_pg:price:pro",
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: 10, Chat: botgotd.Chat{ID: 9001}}},
		},
	})
	if handled := h.HandlePanelInput(ctx, b, 9001, 9001, "50.000", &botgotd.Message{
		ID:   11,
		Chat: botgotd.Chat{ID: 9001},
		From: &botgotd.User{ID: 9001},
		Text: "50.000",
	}); !handled {
		t.Fatalf("expected HandlePanelInput to process owner price input")
	}

	h.OwnerPGCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:      "cb_disc_pro",
			From:    botgotd.User{ID: 9001},
			Data:    "owner_pg:setdisc:pro:20",
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: 12, Chat: botgotd.Chat{ID: 9001}}},
		},
	})

	hReloaded := NewHandler(nil, dm)
	pkg, ok := hReloaded.resolveShopPackage("pro", 3)
	if !ok || pkg.UnitPriceIDR != 50000 || pkg.DiscountPct != 20 || pkg.PriceIDR != 120000 || pkg.DurationDays != 90 {
		t.Fatalf("unexpected persisted pricing with qty=3 & 20%% discount: %+v ok=%v", pkg, ok)
	}

	hReloaded.ShopCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:      "cb_qty_3",
			From:    botgotd.User{ID: 5001},
			Data:    "shop:pkg:pro:3",
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: 20, Chat: botgotd.Chat{ID: 5001}}},
		},
	})
	reqs = rpcLog.getRequests()
	lastEdit := reqs[len(reqs)-1]
	editTxt, _ := lastEdit.Body["text"].(string)
	if !strings.Contains(editTxt, "3x") || !strings.Contains(editTxt, "90 Hari") || !strings.Contains(editTxt, "Rp 120.000") {
		t.Fatalf("expected Qty 3 summary in editMessageText, got %q", editTxt)
	}

	hReloaded.ShopCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:      "cb_buy_3",
			From:    botgotd.User{ID: 5001},
			Data:    "shop:buy:pro:3",
			Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: 20, Chat: botgotd.Chat{ID: 5001}}},
		},
	})
	if gotPkgID != "pro:3" || gotBaseAmount != 120000 {
		t.Fatalf("expected PG invoice package_id=pro:3 base_amount=120000, got pkg=%q amt=%d", gotPkgID, gotBaseAmount)
	}

	cbPayload := []byte(`{"invoice_id":"inv_qty_1","external_id":"shop_5001_qty","buyer_ref":"5001","package_id":"pro:3","status":"PAID","base_amount":120000,"unique_code":111,"total_amount":120111,"paid_at":1795262000,"source_package":"id.dana"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "fedcba9876543210fedcba9876543210"
	sig := signTestPGCallback("cb-secret-qty-123", ts, nonce, cbPayload)
	reqOK := httptest.NewRequest(http.MethodPost, "/internal/shop/payment-callback", bytes.NewReader(cbPayload))
	reqOK.Header.Set("X-PG-Timestamp", ts)
	reqOK.Header.Set("X-PG-Nonce", nonce)
	reqOK.Header.Set("X-PG-Signature", sig)
	recOK := httptest.NewRecorder()
	hReloaded.serveShopPaymentCallback(recOK, reqOK, b, "cb-secret-qty-123")
	if recOK.Code != http.StatusOK {
		t.Fatalf("expected 200 callback, got %d", recOK.Code)
	}

	lic, err := dm.GetActiveUserLicense(ctx, 5001)
	if err != nil || lic == nil {
		t.Fatalf("expected active license after qty=3 fulfill, got %+v err=%v", lic, err)
	}
	if time.Until(lic.ExpiresAt) < 89*24*time.Hour {
		t.Fatalf("expected >= 89 days license duration for 3x Pro, got %v", time.Until(lic.ExpiresAt))
	}
}

func TestShopCancelDeletesMessageAndRateLimit3Per5Min(t *testing.T) {
	t.Setenv("PG_API_KEY", "test-api-key")
	t.Setenv("PG_CALLBACK_SECRET", "cb-secret-rl")

	var createCount atomic.Int32
	pngTiny := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n"))
	pgMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/invoices" && r.Method == http.MethodPost {
			n := createCount.Add(1)
			invID := fmt.Sprintf("inv_rl_%d", n)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":            true,
				"qr_png_base64": pngTiny,
				"invoice": map[string]any{
					"id":           invID,
					"merchant_id":  "default",
					"external_id":  invID,
					"buyer_ref":    "7001",
					"package_id":   "pro:1",
					"base_amount":  45000,
					"unique_code":  int(n),
					"total_amount": 45000 + int64(n),
					"status":       "PENDING",
					"expires_at":   time.Now().Add(15 * time.Minute).Unix(),
				},
			})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/cancel") && r.Method == http.MethodPost {
			invID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/invoices/"), "/cancel")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"invoice": map[string]any{
					"id":           invID,
					"status":       "CANCELLED",
					"total_amount": 45001,
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer pgMock.Close()
	t.Setenv("PG_BASE_URL", pgMock.URL)

	var nextMsgID atomic.Int32
	nextMsgID.Store(200)
	rpcLog := &mockRPCLog{}
	tgMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/sendPhoto") {
			_, _ = io.ReadAll(r.Body)
			id := int(nextMsgID.Add(1))
			rpcLog.add(r.URL.Path, map[string]any{"message_id": id})
			_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d}}`, id)
			return
		}
		var bodyMap map[string]any
		_ = json.NewDecoder(r.Body).Decode(&bodyMap)
		rpcLog.add(r.URL.Path, bodyMap)
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer tgMock.Close()
	b := botgotd.NewTestBot(tgMock.URL, "test_token_rl")

	h := NewHandler(nil, nil)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		h.ShopCallback(ctx, b, &botgotd.Update{
			CallbackQuery: &botgotd.CallbackQuery{
				ID:   fmt.Sprintf("cb_create_%d", i),
				From: botgotd.User{ID: 7001},
				Data: "shop:buy:pro:1",
			},
		})
		invID := fmt.Sprintf("inv_rl_%d", i)
		ord := h.shop.getPending(invID)
		if ord == nil || ord.QRMsgID == 0 {
			t.Fatalf("expected tracked invoice %s with QRMsgID", invID)
		}
		h.ShopCallback(ctx, b, &botgotd.Update{
			CallbackQuery: &botgotd.CallbackQuery{
				ID:      fmt.Sprintf("cb_cancel_%d", i),
				From:    botgotd.User{ID: 7001},
				Data:    "shop:cancel:" + invID,
				Message: &botgotd.MaybeInaccessibleMessage{Message: &botgotd.Message{ID: ord.QRMsgID, Chat: botgotd.Chat{ID: 7001}}},
			},
		})
	}

	if rpcLog.countByMethod("deleteMessages") != 3 {
		t.Fatalf("expected 3 deleteMessages calls on 3 cancellations, got %d", rpcLog.countByMethod("deleteMessages"))
	}
	for _, msg := range extractRPCMessages(rpcLog) {
		if strings.Contains(msg, "Dibatalkan") {
			t.Fatalf("unexpected cancellation text message sent after delete: %q", msg)
		}
	}

	h.ShopCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:   "cb_create_4_blocked",
			From: botgotd.User{ID: 7001},
			Data: "shop:buy:pro:1",
		},
	})
	if createCount.Load() != 3 {
		t.Fatalf("expected 4th order within 5 minutes to be blocked, pg createCount=%d", createCount.Load())
	}
	reqs := rpcLog.getRequests()
	last := reqs[len(reqs)-1]
	txt, _ := last.Body["text"].(string)
	alert, _ := last.Body["show_alert"].(bool)
	if !alert || !strings.Contains(txt, "3x / 5 menit") {
		t.Fatalf("expected rate-limit alert for 4th invoice, got alert=%v text=%q", alert, txt)
	}

	h.ShopCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:   "cb_other_buyer",
			From: botgotd.User{ID: 7002},
			Data: "shop:buy:pro:1",
		},
	})
	if createCount.Load() != 4 {
		t.Fatalf("expected different user 7002 not rate-limited by 7001, createCount=%d", createCount.Load())
	}

	h.shop.mu.Lock()
	for i := range h.shop.orderHistory[7001] {
		h.shop.orderHistory[7001][i] -= 301
	}
	h.shop.mu.Unlock()
	if allowed, _ := h.shop.checkOrderRateLimit(7001, time.Now()); !allowed {
		t.Fatalf("expected rate limit to reset after 5 minutes")
	}
}
