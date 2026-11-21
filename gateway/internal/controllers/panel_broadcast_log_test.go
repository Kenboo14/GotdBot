package controllers

import (
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNotifikasiModesAndBroadcastLogDelivery(t *testing.T) {
	if normalizeNotifikasiMode("Nonaktif") != "Nonaktif" ||
		normalizeNotifikasiMode("Aktif · Kirim ke Saya") != "Kirim ke Saya" ||
		normalizeNotifikasiMode("Kirim ke saya & Admin") != "Kirim ke saya & Admin" {
		t.Fatalf("unexpected normalizeNotifikasiMode output")
	}

	var rpcLog *mockRPCLog
	server, b, logPtr := setupMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if rpcLog != nil {
			reqs := rpcLog.getRequests()
			if len(reqs) > 0 {
				last := reqs[len(reqs)-1]
				if strings.HasSuffix(r.URL.Path, "/sendMessage") {
					if txt, _ := last.Body["text"].(string); strings.Contains(txt, "TRIGGER_PARSE_ERR") {
						if pm, _ := last.Body["parse_mode"].(string); pm == "HTML" {
							w.WriteHeader(http.StatusBadRequest)
							_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`))
							return
						}
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		if strings.HasSuffix(r.URL.Path, "/editMessageText") || strings.HasSuffix(r.URL.Path, "/sendMessage") {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":1700000000,"chat":{"id":1,"type":"private"}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	})
	rpcLog = logPtr
	defer server.Close()

	brk, getPayloads := startMockBroker(t)
	ctx := context.Background()
	dm, err := databases.NewDataManager(ctx, ctx, "", "", filepath.Join(t.TempDir(), "notif_test.db"))
	if err != nil {
		t.Fatalf("NewDataManager failed: %v", err)
	}
	t.Cleanup(func() {
		if sq := dm.GetSQLiteDB(); sq != nil {
			_ = sq.Close()
		}
	})

	h := NewHandler(brk, dm)
	const ownerID int64 = 5001
	const adminID int64 = 5002

	cfg := dm.GetUserConfig(ctx, ownerID)
	cfg.Admin = fmt.Sprintf("%d", adminID)
	if err := dm.SaveUserConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveUserConfig failed: %v", err)
	}

	h.ToggleNotifikasi(ctx, b, ownerID, ownerID)
	if state := h.getPanelState(ownerID); state != "" {
		t.Fatalf("expected empty panel state for RichInline ToggleNotifikasi, got %q", state)
	}
	initMsgs := extractRPCMessages(rpcLog)
	if len(initMsgs) == 0 {
		t.Fatalf("expected ToggleNotifikasi to send initial rich message")
	}
	initMsg := initMsgs[len(initMsgs)-1]
	for _, btnData := range []string{"panel:notif:set:both", "panel:notif:set:self", "panel:notif:set:off"} {
		if !strings.Contains(initMsg, btnData) {
			t.Fatalf("expected RichInline button %q in ToggleNotifikasi response", btnData)
		}
	}

	for _, mode := range []string{"Kirim ke saya & Admin", "Kirim ke Saya", "Nonaktif"} {
		rendered := buildNotifikasiHTML(mode)
		if _, stripped := buildStripedRichMessage(rendered); stripped {
			t.Fatalf("expected buildStripedRichMessage to bypass <tg-button-row> for mode %q", mode)
		}
		for _, btnData := range []string{"panel:notif:set:both", "panel:notif:set:self", "panel:notif:set:off"} {
			if !strings.Contains(rendered, btnData) {
				t.Fatalf("expected RichInline button %q in rendered HTML for mode %q", btnData, mode)
			}
		}
	}

	sendBeforeCB := rpcLog.countByMethod("sendMessage")
	editBeforeCB := rpcLog.countByMethod("editMessageText")

	h.PanelCallback(ctx, b, makeTestCBUpdate(ownerID, 19, "panel:notif:set:both"))
	if got := dm.GetUserConfig(ctx, ownerID).Notifikasi; got != "Kirim ke saya & Admin" {
		t.Fatalf("expected Kirim ke saya & Admin, got %q", got)
	}
	if len(getPayloads()) == 0 {
		t.Fatalf("expected sync_panel_config payload to be sent")
	}
	if rpcLog.countByMethod("sendMessage") != sendBeforeCB {
		t.Fatalf("expected 0 new sendMessage calls when toggling RichInline notifikasi")
	}
	if rpcLog.countByMethod("editMessageText") != editBeforeCB+1 {
		t.Fatalf("expected 1 editMessageText call after selecting panel:notif:set:both")
	}
	editedMsgs := extractRPCMessages(rpcLog)
	lastEdited := editedMsgs[len(editedMsgs)-1]
	if !strings.Contains(lastEdited, "Kirim ke saya &amp; Admin") {
		t.Fatalf("expected updated mode label in edited message: %q", lastEdited)
	}

	beforeCount := len(rpcLog.getRequests())
	h.handlePanelBroadcastLog(ctx, b, models.PFromService{
		ChatID:  ownerID,
		OwnerID: ownerID,
		Type:    "panel_broadcast_log",
		Text:    "<b>Putaran :</b> <i>1</i>",
	})
	reqs := rpcLog.getRequests()[beforeCount:]
	sentTargets := make(map[string]bool)
	for _, r := range reqs {
		if strings.HasSuffix(r.Path, "/sendMessage") {
			sentTargets[fmt.Sprintf("%v", r.Body["chat_id"])] = true
		}
	}
	if !sentTargets["5001"] || !sentTargets["5002"] {
		t.Fatalf("expected log sent to owner 5001 and admin 5002, got %v", sentTargets)
	}

	sendBeforeSelf := rpcLog.countByMethod("sendMessage")
	h.PanelCallback(ctx, b, makeTestCBUpdate(ownerID, 20, "panel:notif:set:self"))
	if got := dm.GetUserConfig(ctx, ownerID).Notifikasi; got != "Kirim ke Saya" {
		t.Fatalf("expected Kirim ke Saya, got %q", got)
	}
	if rpcLog.countByMethod("sendMessage") != sendBeforeSelf {
		t.Fatalf("expected 0 new sendMessage calls when selecting panel:notif:set:self")
	}
	beforeSelf := len(rpcLog.getRequests())
	h.handlePanelBroadcastLog(ctx, b, models.PFromService{
		ChatID:  ownerID,
		OwnerID: ownerID,
		Type:    "panel_broadcast_log",
		Text:    "<b>TRIGGER_PARSE_ERR</b>",
	})
	selfReqs := rpcLog.getRequests()[beforeSelf:]
	selfSent := make(map[string]int)
	for _, r := range selfReqs {
		if strings.HasSuffix(r.Path, "/sendMessage") {
			cid := fmt.Sprintf("%v", r.Body["chat_id"])
			selfSent[cid]++
		}
	}
	if selfSent["5001"] < 2 || selfSent["5002"] != 0 {
		t.Fatalf("expected HTML+fallback plain retry to 5001 and 0 to admin 5002, got %v", selfSent)
	}

	sendBeforeOff := rpcLog.countByMethod("sendMessage")
	h.PanelCallback(ctx, b, makeTestCBUpdate(ownerID, 21, "panel:notif:set:off"))
	if got := dm.GetUserConfig(ctx, ownerID).Notifikasi; got != "Nonaktif" {
		t.Fatalf("expected Nonaktif, got %q", got)
	}
	if rpcLog.countByMethod("sendMessage") != sendBeforeOff {
		t.Fatalf("expected 0 new sendMessage calls when selecting panel:notif:set:off")
	}
	beforeOff := len(rpcLog.getRequests())
	h.handlePanelBroadcastLog(ctx, b, models.PFromService{
		ChatID:  ownerID,
		OwnerID: ownerID,
		Type:    "panel_broadcast_log",
		Text:    "<b>Should Not Send</b>",
	})
	if len(rpcLog.getRequests()) != beforeOff {
		t.Fatalf("expected 0 RPC calls when Notifikasi is Nonaktif")
	}
}

func TestPindahAksesAndOldSessionRevocationOnInstall(t *testing.T) {
	var rpcLog *mockRPCLog
	server, b, logPtr := setupMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if rpcLog != nil {
			reqs := rpcLog.getRequests()
			if len(reqs) > 0 {
				last := reqs[len(reqs)-1]
				if strings.HasSuffix(r.URL.Path, "/sendMessage") && fmt.Sprintf("%v", last.Body["chat_id"]) == "6099" {
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"ok":false,"error_code":403,"description":"Forbidden: bot can't initiate conversation with a user"}`))
					return
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		if strings.HasSuffix(r.URL.Path, "/editMessageText") || strings.HasSuffix(r.URL.Path, "/sendMessage") {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":1700000000,"chat":{"id":1,"type":"private"}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	})
	rpcLog = logPtr
	defer server.Close()

	brk, getPayloads := startMockBroker(t)
	ctx := context.Background()
	dm, err := databases.NewDataManager(ctx, ctx, "", "", filepath.Join(t.TempDir(), "transfer_test.db"))
	if err != nil {
		t.Fatalf("NewDataManager failed: %v", err)
	}
	t.Cleanup(func() {
		if sq := dm.GetSQLiteDB(); sq != nil {
			_ = sq.Close()
		}
	})

	h := NewHandler(brk, dm)
	const oldOwner int64 = 6001
	const newOwner int64 = 6002
	const unreachableOwner int64 = 6099

	proSHA := ComputeTokenSHA(GenerateMasterToken(), oldOwner)
	proPlusSHA := ComputeTokenSHA(GenerateMasterToken(), oldOwner)
	_ = dm.SaveUserLicense(ctx, &databases.UserLicense{
		TokenSHA:    proSHA,
		MasterToken: GenerateMasterToken(),
		OwnerChatID: oldOwner,
		UserbotID:   oldOwner,
		Tier:        "Pro",
		ExpiresAt:   time.Now().Add(10 * 24 * time.Hour),
		CreatedAt:   time.Now(),
		Status:      "active",
	})
	_ = dm.SaveUserLicense(ctx, &databases.UserLicense{
		TokenSHA:           proPlusSHA,
		MasterToken:        GenerateMasterToken(),
		OwnerChatID:        oldOwner,
		UserbotID:          oldOwner,
		Tier:               "Pro++",
		CanCustomWatermark: true,
		ExpiresAt:          time.Now().Add(20 * 24 * time.Hour),
		CreatedAt:          time.Now(),
		Status:             "active",
	})
	cfg := dm.GetUserConfig(ctx, oldOwner)
	cfg.ActiveTokenSHA = proPlusSHA
	cfg.Status = "Berjalan"
	_ = dm.SaveUserConfig(ctx, cfg)
	_, err = dm.GetSQLiteDB().ExecContext(ctx, `INSERT INTO sessions (chat_id, owner_id, account_id, session_blob) VALUES (?, ?, ?, ?)`, oldOwner, oldOwner, oldOwner, "enc")
	if err != nil {
		t.Fatalf("insert mock session failed: %v", err)
	}

	h.HandlePindahAkses(ctx, b, oldOwner, oldOwner)
	if state := h.getPanelState(oldOwner); state != "waiting_transfer_token_target" {
		t.Fatalf("expected waiting_transfer_token_target state, got %q", state)
	}

	if !h.HandlePanelInput(ctx, b, oldOwner, oldOwner, fmt.Sprintf("%d", unreachableOwner), nil) {
		t.Fatalf("expected transfer target input for unreachable user to be handled gracefully")
	}
	if !dm.HasActiveSession(ctx, oldOwner) {
		t.Fatalf("oldOwner session must NOT be revoked before new owner installs userbot")
	}

	h.HandlePindahAkses(ctx, b, oldOwner, oldOwner)
	if !h.HandlePanelInput(ctx, b, oldOwner, oldOwner, fmt.Sprintf("%d", newOwner), nil) {
		t.Fatalf("expected transfer target input for newOwner to succeed")
	}

	h.PanelCallback(ctx, b, makeTestCBUpdate(newOwner, 30, "panel:transfer:claim"))
	h.Sessions.mu.RLock()
	claimed := h.Sessions.sessions[newOwner]
	h.Sessions.mu.RUnlock()
	if claimed == nil || claimed.State != "waiting_phone" || claimed.OldOwnerID != oldOwner || claimed.PendingTokenSHA != proPlusSHA {
		t.Fatalf("expected newOwner session waiting_phone with oldOwner=%d, got %+v", oldOwner, claimed)
	}

	licTransfer, _ := dm.GetUserLicenseBySHA(ctx, proPlusSHA)
	newLic, txErr := h.transferTokenSlotBetweenAccounts(ctx, oldOwner, newOwner, newOwner, licTransfer)
	if txErr != nil || newLic == nil {
		t.Fatalf("transferTokenSlotBetweenAccounts failed: %v", txErr)
	}
	if dm.HasActiveSession(ctx, oldOwner) {
		t.Fatalf("expected oldOwner session to be deleted after successful transfer")
	}
	if status := dm.GetUserConfig(ctx, oldOwner).Status; status != "Berhenti" {
		t.Fatalf("expected oldOwner broadcast Status=Berhenti after transfer, got %q", status)
	}

	foundRevoke := false
	for _, p := range getPayloads() {
		if p.Type == "revoke_transferred_session" && p.OwnerID == oldOwner {
			foundRevoke = true
		}
	}
	if !foundRevoke {
		t.Fatalf("expected revoke_transferred_session broker command for oldOwner=%d", oldOwner)
	}
}
