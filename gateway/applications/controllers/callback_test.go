package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	t_models "github.com/go-telegram/bot/models"
)

// ──────────────────────────────────────────────────────
// Unit Tests: Routing, Cache, & Error Handling
// ──────────────────────────────────────────────────────

func TestHelpPageCache_HitAndFallback(t *testing.T) {
	expectedKeys := []string{
		"assist_help:home",
		"assist_help:ping",
		"assist_help:notes",
		"assist_help:code",
		"assist_help:server",
	}

	for _, k := range expectedKeys {
		page, ok := cachedHelpPages[k]
		if !ok {
			t.Errorf("expected cachedHelpPages to have %q", k)
		}
		if strings.TrimSpace(page.text) == "" {
			t.Errorf("cached text for %q must not be empty", k)
		}
		if page.markup == nil {
			t.Errorf("cached markup for %q must not be nil", k)
		}
	}

	uncachedKey := "assist_help:admin"
	_, ok := cachedHelpPages[uncachedKey]
	if ok {
		t.Errorf("key %q should not be in static map", uncachedKey)
	}
	category := strings.TrimPrefix(uncachedKey, "assist_help:")
	if category != "admin" {
		t.Errorf("expected category admin, got %q", category)
	}
}

func TestIsMessageNotModifiedErr(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{"nil error", nil, false},
		{"generic error", errors.New("connection reset by peer"), false},
		{"exact telegram message", errors.New("Bad Request: message is not modified: specified new message content and reply markup are exactly the same as a current content and reply markup of the message"), true},
		{"partial lower", errors.New("bad request: message is not modified"), true},
		{"underscore format", errors.New("MESSAGE_NOT_MODIFIED"), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isMessageNotModifiedErr(tc.err)
			if got != tc.expect {
				t.Errorf("isMessageNotModifiedErr(%v) = %v, want %v", tc.err, got, tc.expect)
			}
		})
	}
}

func TestNoteCallback_DataParsing(t *testing.T) {
	tests := []struct {
		name        string
		data        string
		valid       bool
		expectAct   string
		expectOwner int64
		expectNote  string
	}{
		{"valid show", "note:show:12345:welcome", true, "show", 12345, "welcome"},
		{"valid pop", "note:pop:999:help", true, "pop", 999, "help"},
		{"invalid parts short", "note:show:123", false, "", 0, ""},
		{"invalid parts single", "note", false, "", 0, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parts := strings.Split(tc.data, ":")
			if len(parts) < 4 {
				if tc.valid {
					t.Fatalf("expected valid parsing for %q", tc.data)
				}
				return
			}
			if !tc.valid {
				t.Fatalf("expected invalid parsing for %q", tc.data)
			}

			action := parts[1]
			if action != tc.expectAct {
				t.Errorf("action = %q, want %q", action, tc.expectAct)
			}

			name := strings.ToLower(parts[3])
			if name != tc.expectNote {
				t.Errorf("note name = %q, want %q", name, tc.expectNote)
			}
		})
	}
}

// ──────────────────────────────────────────────────────
// Mock Telegram Server & RPC Verification Tests
// ──────────────────────────────────────────────────────

type mockRPCLog struct {
	mu       sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	Path string
	Body map[string]any
}

func (m *mockRPCLog) add(path string, body map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, recordedRequest{Path: path, Body: body})
}

func (m *mockRPCLog) countByMethod(method string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := 0
	for _, r := range m.requests {
		if strings.HasSuffix(r.Path, "/"+method) {
			c++
		}
	}
	return c
}

func (m *mockRPCLog) findRequestByMethod(method string) (recordedRequest, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.requests {
		if strings.HasSuffix(r.Path, "/"+method) {
			return r, true
		}
	}
	return recordedRequest{}, false
}

func (m *mockRPCLog) getRequests() []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	dst := make([]recordedRequest, len(m.requests))
	copy(dst, m.requests)
	return dst
}

func setupMockTelegramServer(t *testing.T, customHandler http.HandlerFunc) (*httptest.Server, *bot.Bot, *mockRPCLog) {
	rpcLog := &mockRPCLog{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		bodyMap := make(map[string]any)
		ct := r.Header.Get("Content-Type")
		if strings.Contains(ct, "multipart/form-data") {
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			if err := r.ParseMultipartForm(32 << 20); err == nil && r.MultipartForm != nil {
				for k, v := range r.MultipartForm.Value {
					if len(v) > 0 {
						if v[0] == "true" {
							bodyMap[k] = true
						} else if v[0] == "false" {
							bodyMap[k] = false
						} else {
							bodyMap[k] = v[0]
						}
					}
				}
			}
		} else {
			_ = json.Unmarshal(bodyBytes, &bodyMap)
		}
		rpcLog.add(r.URL.Path, bodyMap)

		if strings.HasSuffix(r.URL.Path, "/getMe") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":12345,"is_bot":true,"first_name":"GotdBot","username":"gotd_bot"}}`))
			return
		}

		if customHandler != nil {
			customHandler(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":1700000000,"chat":{"id":1,"type":"private"}}}`))
		} else {
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))

	b, err := bot.New("test_token_123", bot.WithServerURL(server.URL))
	if err != nil {
		t.Fatalf("failed to create bot: %v", err)
	}
	return server, b, rpcLog
}

// TestNoteCallback_Pop_ExactOneACK verifies that a popup callback sends
// exactly 1 AnswerCallbackQuery RPC with ShowAlert=true, and 0 EditMessageText RPCs.
func TestNoteCallback_Pop_ExactOneACK(t *testing.T) {
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	h := &Handler{}
	update := &t_models.Update{
		CallbackQuery: &t_models.CallbackQuery{
			ID:              "cb_pop_001",
			Data:            "note:pop:1001:rules",
			InlineMessageID: "inl_msg_101",
		},
	}

	h.NoteCallback(context.Background(), b, update)

	ackCount := rpcLog.countByMethod("answerCallbackQuery")
	editCount := rpcLog.countByMethod("editMessageText")

	if ackCount != 1 {
		t.Errorf("expected exactly 1 answerCallbackQuery, got %d", ackCount)
	}
	if editCount != 0 {
		t.Errorf("expected 0 editMessageText for modal pop, got %d", editCount)
	}

	// Verify that showAlert was true
	req, found := rpcLog.findRequestByMethod("answerCallbackQuery")
	if !found {
		t.Fatalf("answerCallbackQuery request not found")
	}
	t.Logf("answerCallbackQuery body: %+v", req.Body)
	showAlert, _ := req.Body["show_alert"].(bool)
	if !showAlert {
		t.Errorf("expected show_alert=true for note:pop, got %v", showAlert)
	}
}

// TestNoteCallback_Show_OneACKOneEdit verifies that a show callback triggers
// exactly 1 AnswerCallbackQuery (spinner dismiss) and 1 EditMessageText.
func TestNoteCallback_Show_OneACKOneEdit(t *testing.T) {
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	h := &Handler{}
	update := &t_models.Update{
		CallbackQuery: &t_models.CallbackQuery{
			ID:              "cb_show_002",
			Data:            "note:show:1001:rules",
			InlineMessageID: "inl_msg_102",
		},
	}

	h.NoteCallback(context.Background(), b, update)

	// Wait up to 500ms for background ACK goroutine to deliver
	time.Sleep(50 * time.Millisecond)

	ackCount := rpcLog.countByMethod("answerCallbackQuery")
	editCount := rpcLog.countByMethod("editMessageText")

	if ackCount != 1 {
		t.Errorf("expected exactly 1 answerCallbackQuery, got %d", ackCount)
	}
	if editCount != 1 {
		t.Errorf("expected exactly 1 editMessageText, got %d", editCount)
	}
}

// TestNoteCallback_InvalidData_ExactOneAlert verifies invalid callback data produces
// exactly 1 AnswerCallbackQuery alert and no edits.
func TestNoteCallback_InvalidData_ExactOneAlert(t *testing.T) {
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	h := &Handler{}
	update := &t_models.Update{
		CallbackQuery: &t_models.CallbackQuery{
			ID:   "cb_invalid_003",
			Data: "note:malformed",
		},
	}

	h.NoteCallback(context.Background(), b, update)

	ackCount := rpcLog.countByMethod("answerCallbackQuery")
	editCount := rpcLog.countByMethod("editMessageText")

	if ackCount != 1 {
		t.Errorf("expected exactly 1 answerCallbackQuery alert, got %d", ackCount)
	}
	if editCount != 0 {
		t.Errorf("expected 0 editMessageText for invalid callback data, got %d", editCount)
	}
}

// TestHelpCallback_OneACKOneEdit verifies help page navigation dispatches
// 1 ACK and performs 1 EditMessageText.
func TestHelpCallback_OneACKOneEdit(t *testing.T) {
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	h := &Handler{}
	update := &t_models.Update{
		CallbackQuery: &t_models.CallbackQuery{
			ID:              "cb_help_004",
			Data:            "assist_help:notes",
			InlineMessageID: "inl_msg_104",
		},
	}

	h.HelpCallback(context.Background(), b, update)

	// Wait for background ACK goroutine
	time.Sleep(50 * time.Millisecond)

	ackCount := rpcLog.countByMethod("answerCallbackQuery")
	editCount := rpcLog.countByMethod("editMessageText")

	if ackCount != 1 {
		t.Errorf("expected exactly 1 answerCallbackQuery, got %d", ackCount)
	}
	if editCount != 1 {
		t.Errorf("expected exactly 1 editMessageText, got %d", editCount)
	}
}

// TestServerCallback_EarlyACK verifies server status refresh sends early ACK
// and edits the message.
func TestServerCallback_EarlyACK(t *testing.T) {
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	h := &Handler{}
	update := &t_models.Update{
		CallbackQuery: &t_models.CallbackQuery{
			ID:              "cb_server_005",
			Data:            "assist_server:refresh",
			InlineMessageID: "inl_msg_105",
		},
	}

	h.serverCallback(context.Background(), b, update)

	time.Sleep(50 * time.Millisecond)

	ackCount := rpcLog.countByMethod("answerCallbackQuery")
	editCount := rpcLog.countByMethod("editMessageText")

	if ackCount != 1 {
		t.Errorf("expected exactly 1 answerCallbackQuery, got %d", ackCount)
	}
	if editCount != 1 {
		t.Errorf("expected exactly 1 editMessageText, got %d", editCount)
	}
}

// TestMessageNotModified_NonFatal verifies that when Telegram returns 400
// "Bad Request: message is not modified", the callback handler treats it as non-fatal.
func TestMessageNotModified_NonFatal(t *testing.T) {
	customHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: message is not modified: specified new message content and reply markup are exactly the same as a current content and reply markup of the message"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}

	server, b, _ := setupMockTelegramServer(t, customHandler)
	defer server.Close()

	h := &Handler{}
	update := &t_models.Update{
		CallbackQuery: &t_models.CallbackQuery{
			ID:              "cb_not_modified_006",
			Data:            "assist_help:notes",
			InlineMessageID: "inl_msg_106",
		},
	}

	// Must not panic or return fatal error
	h.HelpCallback(context.Background(), b, update)
}

// TestCallback_ConcurrentSafe verifies thread safety across multiple simultaneous callbacks.
func TestCallback_ConcurrentSafe(t *testing.T) {
	server, b, _ := setupMockTelegramServer(t, nil)
	defer server.Close()

	h := &Handler{}
	var wg sync.WaitGroup
	var completedCount atomic.Int32

	concurrency := 40
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func(id int) {
			defer wg.Done()
			update := &t_models.Update{
				CallbackQuery: &t_models.CallbackQuery{
					ID:              fmt.Sprintf("cb_conc_%d", id),
					Data:            "assist_help:ping",
					InlineMessageID: fmt.Sprintf("inl_msg_%d", id),
				},
			}
			h.HelpCallback(context.Background(), b, update)
			completedCount.Add(1)
		}(i)
	}

	wg.Wait()
	if int(completedCount.Load()) != concurrency {
		t.Errorf("expected %d completed callbacks, got %d", concurrency, completedCount.Load())
	}
}

// ──────────────────────────────────────────────────────
// Benchmarks: In-Memory Static Cache vs Real Dynamic Formatting
// ──────────────────────────────────────────────────────

// Global sinks to prevent compiler dead-code elimination
var (
	benchSinkText   string
	benchSinkMarkup *t_models.InlineKeyboardMarkup
)

// BenchmarkHelpPage_StaticCacheLookup measures O(1) in-memory map lookup.
func BenchmarkHelpPage_StaticCacheLookup(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page := cachedHelpPages["assist_help:notes"]
		benchSinkText = page.text
		benchSinkMarkup = page.markup
	}
}

// BenchmarkHelpPage_DynamicFallbackFormatting measures real dynamic string formatting
// using strings.TrimPrefix, html.EscapeString, and fmt.Sprintf (the fallback path).
func BenchmarkHelpPage_DynamicFallbackFormatting(b *testing.B) {
	data := "assist_help:moderation"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		category := strings.TrimPrefix(data, "assist_help:")
		text := fmt.Sprintf("<b>%s</b>\n\nKategori ini tersedia pada menu bantuan.", html.EscapeString(category))
		markup := cachedHelpBackMarkup
		benchSinkText = text
		benchSinkMarkup = markup
	}
}

// BenchmarkHelpPage_LegacyTemplateFormatting measures legacy full-text template formatting
// with multiple allocations per request.
func BenchmarkHelpPage_LegacyTemplateFormatting(b *testing.B) {
	category := "notes"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		text := fmt.Sprintf("<b>%s</b>\n\nPerintah catatan:\n<code>!save &lt;nama&gt; [-cb|-pop]</code> — Simpan catatan\n<code>!get &lt;nama&gt;</code> atau <code>#&lt;nama&gt;</code> — Ambil catatan\n<code>!lsnote</code> — Daftar catatan\n<code>!delnote &lt;nama&gt;</code> — Hapus catatan", strings.ToUpper(category))
		markup := cachedHelpBackMarkup
		benchSinkText = text
		benchSinkMarkup = markup
	}
}
