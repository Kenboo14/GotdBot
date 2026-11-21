package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func signTestPGCallback(secret, ts, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(ts + "." + nonce + "." + string(body)))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestShopCheckoutCallbackAndAutoFulfill(t *testing.T) {
	pngTiny := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n"))
	pgMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/invoices" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":            true,
				"qr_png_base64": pngTiny,
				"invoice": map[string]any{
					"id":           "inv_test_1",
					"merchant_id":  "default",
					"external_id":  "shop_8881_1",
					"buyer_ref":    "8881",
					"package_id":   "pro_30d",
					"base_amount":  45000,
					"unique_code":  137,
					"total_amount": 45137,
					"status":       "PENDING",
					"expires_at":   time.Now().Add(15 * time.Minute).Unix(),
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer pgMock.Close()

	t.Setenv("PG_BASE_URL", pgMock.URL)
	t.Setenv("PG_API_KEY", "test-api-key")
	t.Setenv("PG_CALLBACK_SECRET", "cb-secret-key-123")
	t.Setenv("PG_CALLBACK_URL", "http://127.0.0.1:8085/internal/shop/payment-callback")

	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "shop_test.db")
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
	const buyerID int64 = 8881

	h.ShowShop(ctx, b, buyerID)
	h.handleShopBuy(ctx, b, buyerID, buyerID, "cb-1", "pro_30d")

	ord := h.shop.getActiveForBuyer(buyerID, time.Now())
	if ord == nil || ord.InvoiceID != "inv_test_1" || ord.TotalAmount != 45137 {
		t.Fatalf("expected tracked pending shop order, got %+v", ord)
	}

	cbPayload := []byte(`{"invoice_id":"inv_test_1","external_id":"shop_8881_1","buyer_ref":"8881","package_id":"pro_30d","status":"PAID","base_amount":45000,"unique_code":137,"total_amount":45137,"paid_at":1795230602,"source_package":"com.gojek.gobiz"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "0123456789abcdef0123456789abcdef"
	sig := signTestPGCallback("cb-secret-key-123", ts, nonce, cbPayload)

	reqBad := httptest.NewRequest(http.MethodPost, "/internal/shop/payment-callback", bytes.NewReader(cbPayload))
	reqBad.Header.Set("X-PG-Timestamp", ts)
	reqBad.Header.Set("X-PG-Nonce", nonce)
	reqBad.Header.Set("X-PG-Signature", "deadbeef")
	recBad := httptest.NewRecorder()
	h.serveShopPaymentCallback(recBad, reqBad, b, "cb-secret-key-123")
	if recBad.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on bad HMAC signature, got %d", recBad.Code)
	}

	reqOK := httptest.NewRequest(http.MethodPost, "/internal/shop/payment-callback", bytes.NewReader(cbPayload))
	reqOK.Header.Set("X-PG-Timestamp", ts)
	reqOK.Header.Set("X-PG-Nonce", nonce)
	reqOK.Header.Set("X-PG-Signature", sig)
	recOK := httptest.NewRecorder()
	h.serveShopPaymentCallback(recOK, reqOK, b, "cb-secret-key-123")
	if recOK.Code != http.StatusOK {
		t.Fatalf("expected 200 on verified callback, got %d (%s)", recOK.Code, recOK.Body.String())
	}

	lic, err := dm.GetActiveUserLicense(ctx, buyerID)
	if err != nil || lic == nil || lic.Tier != "Pro" {
		t.Fatalf("expected active Pro license granted to buyer, got %+v err=%v", lic, err)
	}

	reqReplay := httptest.NewRequest(http.MethodPost, "/internal/shop/payment-callback", bytes.NewReader(cbPayload))
	reqReplay.Header.Set("X-PG-Timestamp", ts)
	reqReplay.Header.Set("X-PG-Nonce", nonce)
	reqReplay.Header.Set("X-PG-Signature", sig)
	recReplay := httptest.NewRecorder()
	h.serveShopPaymentCallback(recReplay, reqReplay, b, "cb-secret-key-123")
	if recReplay.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on nonce replay, got %d", recReplay.Code)
	}

	if rpcLog.countByMethod("sendPhoto") != 1 {
		t.Fatalf("expected 1 QRIS sendPhoto request, got %d", rpcLog.countByMethod("sendPhoto"))
	}

	msgs := extractRPCMessages(rpcLog)
	foundSuccess := false
	for _, msg := range msgs {
		if strings.Contains(msg, "Pembayaran Berhasil") && strings.Contains(msg, "GOTD:M") {
			foundSuccess = true
			break
		}
	}
	if !foundSuccess {
		t.Fatalf("expected confirmation message containing Pembayaran Berhasil and GOTD:M voucher in msgs=%v", msgs)
	}
}

func TestOwnerHelpPGUploadQRISPhotoAndText(t *testing.T) {
	t.Setenv("OWNER", "9001")
	t.Setenv("PG_API_KEY", "api-key")
	t.Setenv("PG_ADMIN_KEY", "admin-key")
	t.Setenv("PG_MERCHANT_ID", "default")

	var gotImageB64 string
	var gotStaticText string
	pgMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/admin/merchants" && r.Method == http.MethodPost {
			var body struct {
				StaticQRIS    string `json:"static_qris"`
				QRImageBase64 string `json:"qr_image_base64"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotImageB64 = body.QRImageBase64
			gotStaticText = body.StaticQRIS
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":            true,
				"merchant_name": "TOKO MAJU",
				"merchant_city": "JAKARTA",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer pgMock.Close()
	t.Setenv("PG_BASE_URL", pgMock.URL)

	pngBytes := []byte("\x89PNG\r\n\x1a\nFAKE_QR_IMAGE_BYTES")
	tgMock := httptest.NewServer(nil)
	var b *botgotd.Bot
	rpcLog := &mockRPCLog{}
	tgMock.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bottest_token_123/getFile":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/qris.png"}}`))
		case "/file/bottest_token_123/photos/qris.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		default:
			var bodyMap map[string]any
			_ = json.NewDecoder(r.Body).Decode(&bodyMap)
			rpcLog.add(r.URL.Path, bodyMap)
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	})
	defer tgMock.Close()
	b = botgotd.NewTestBot(tgMock.URL, "test_token_123")

	h := NewHandler(nil, nil)
	ctx := context.Background()

	h.OwnerPGCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:   "cb_pg_upload",
			From: botgotd.User{ID: 9001},
			Data: "owner_pg:upload",
			Message: &botgotd.MaybeInaccessibleMessage{
				Message: &botgotd.Message{ID: 55, Chat: botgotd.Chat{ID: 9001}},
			},
		},
	})
	if h.getPanelState(9001) != ownerPGUploadState {
		t.Fatalf("expected panel state %q, got %q", ownerPGUploadState, h.getPanelState(9001))
	}

	h.HandlePanelMedia(ctx, b, &botgotd.Update{
		Message: &botgotd.Message{
			ID:    56,
			Chat:  botgotd.Chat{ID: 9001},
			From:  &botgotd.User{ID: 9001},
			Photo: []botgotd.PhotoSize{{FileID: "photo_file_1"}},
		},
	})
	if h.getPanelState(9001) != "" {
		t.Fatalf("expected panel state cleared after successful QRIS upload")
	}
	if gotImageB64 != base64.StdEncoding.EncodeToString(pngBytes) || gotStaticText != "" {
		t.Fatalf("unexpected uploaded merchant QR payload: b64Len=%d static=%q", len(gotImageB64), gotStaticText)
	}
	msgs := extractRPCMessages(rpcLog)
	if len(msgs) == 0 || !strings.Contains(msgs[len(msgs)-1], "QRIS Statis Berhasil Dipasang") || !strings.Contains(msgs[len(msgs)-1], "TOKO MAJU") {
		t.Fatalf("expected confirmation message with merchant name, got %+v", msgs)
	}
}
