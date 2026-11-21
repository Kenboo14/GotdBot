package controllers

import (
	"assterisk/gateway/internal/databases"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func setupAdminHTTPTest(t *testing.T) (*Handler, http.Handler, string) {
	t.Helper()
	t.Setenv("OWNER", "9001")
	adminKey := "test-dashboard-internal-key-99"
	t.Setenv("DASHBOARD_ADMIN_KEY", adminKey)

	dbPath := filepath.Join(t.TempDir(), "admin_http.db")
	dm, err := databases.NewDataManager(context.Background(), context.Background(), "", "", dbPath)
	if err != nil {
		t.Fatalf("NewDataManager failed: %v", err)
	}
	t.Cleanup(func() {
		if sq := dm.GetSQLiteDB(); sq != nil {
			_ = sq.Close()
		}
	})

	h := NewHandler(nil, dm)
	mux := http.NewServeMux()
	h.registerAdminHTTPRoutes(mux, nil)
	return h, mux, adminKey
}

func doAdminReq(t *testing.T, h http.Handler, method, path, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if key != "" {
		req.Header.Set("X-Internal-Admin-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAdminHTTPAuthAndMagic(t *testing.T) {
	_, mux, key := setupAdminHTTPTest(t)

	recUnauth := doAdminReq(t, mux, http.MethodGet, "/internal/admin/overview", "", nil)
	if recUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without key, got %d", recUnauth.Code)
	}
	recWrong := doAdminReq(t, mux, http.MethodGet, "/internal/admin/overview", "wrong-key", nil)
	if recWrong.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 wrong key, got %d", recWrong.Code)
	}

	code, _ := globalDashMagic.Issue(9001)
	recMagic := doAdminReq(t, mux, http.MethodPost, "/internal/admin/auth/verify-magic", key, map[string]string{"code": code})
	if recMagic.Code != http.StatusOK {
		t.Fatalf("expected 200 on first verify-magic, got %d (%s)", recMagic.Code, recMagic.Body.String())
	}
	recReuse := doAdminReq(t, mux, http.MethodPost, "/internal/admin/auth/verify-magic", key, map[string]string{"code": code})
	if recReuse.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on magic code reuse, got %d", recReuse.Code)
	}
}

func TestAdminHTTPPluginsPricingAndTokens(t *testing.T) {
	_, mux, key := setupAdminHTTPTest(t)

	recPlug := doAdminReq(t, mux, http.MethodGet, "/internal/admin/plugins?uid=0", key, nil)
	if recPlug.Code != http.StatusOK {
		t.Fatalf("get plugins failed: %d", recPlug.Code)
	}
	var plugRes struct {
		OK      bool                       `json:"ok"`
		Modules []adminPluginModulePayload `json:"modules"`
	}
	_ = json.Unmarshal(recPlug.Body.Bytes(), &plugRes)
	if !plugRes.OK || len(plugRes.Modules) != len(ownerPluginSpecs) {
		t.Fatalf("expected %d plugin modules, got %d", len(ownerPluginSpecs), len(plugRes.Modules))
	}

	recToggle := doAdminReq(t, mux, http.MethodPost, "/internal/admin/plugins/toggle", key, map[string]any{
		"uid":    0,
		"action": "module",
		"module": "notes",
	})
	if recToggle.Code != http.StatusOK {
		t.Fatalf("toggle module failed: %d (%s)", recToggle.Code, recToggle.Body.String())
	}

	price := int64(55000)
	disc := 15
	recPutShop := doAdminReq(t, mux, http.MethodPut, "/internal/admin/shop/pricing", key, map[string]any{
		"target":       "pro",
		"price_idr":    price,
		"discount_pct": disc,
	})
	if recPutShop.Code != http.StatusOK {
		t.Fatalf("put shop pricing failed: %d (%s)", recPutShop.Code, recPutShop.Body.String())
	}

	recGen := doAdminReq(t, mux, http.MethodPost, "/internal/admin/tokens/generate", key, map[string]string{
		"tier":     "pro++",
		"duration": "30h",
	})
	if recGen.Code != http.StatusOK {
		t.Fatalf("generate token failed: %d (%s)", recGen.Code, recGen.Body.String())
	}
	var genRes struct {
		OK     bool `json:"ok"`
		Master struct {
			Token string `json:"Token"`
			Tier  string `json:"Tier"`
		} `json:"master"`
	}
	_ = json.Unmarshal(recGen.Body.Bytes(), &genRes)
	if !genRes.OK || !IsMasterTokenPrefix(genRes.Master.Token) {
		t.Fatalf("unexpected generated token: %+v", genRes)
	}

	recLook := doAdminReq(t, mux, http.MethodGet, "/internal/admin/tokens/lookup?q="+genRes.Master.Token, key, nil)
	if recLook.Code != http.StatusOK {
		t.Fatalf("lookup master token failed: %d (%s)", recLook.Code, recLook.Body.String())
	}

	recUp := doAdminReq(t, mux, http.MethodPost, "/internal/admin/tokens/upgrade", key, map[string]any{
		"uid":      551234,
		"tier":     "pro++",
		"duration": "30h",
	})
	if recUp.Code != http.StatusOK {
		t.Fatalf("upgrade user license failed: %d (%s)", recUp.Code, recUp.Body.String())
	}

	recExt := doAdminReq(t, mux, http.MethodPost, "/internal/admin/tokens/extend", key, map[string]any{
		"uid":      551234,
		"duration": "7h",
	})
	if recExt.Code != http.StatusOK {
		t.Fatalf("extend user license failed: %d (%s)", recExt.Code, recExt.Body.String())
	}

	recRev := doAdminReq(t, mux, http.MethodPost, "/internal/admin/tokens/revoke", key, map[string]string{
		"query": "551234",
	})
	if recRev.Code != http.StatusOK {
		t.Fatalf("revoke user license failed: %d (%s)", recRev.Code, recRev.Body.String())
	}
}
