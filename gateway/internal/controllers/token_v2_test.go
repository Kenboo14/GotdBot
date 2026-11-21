package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTierPolicyAndBetterLicensePriority(t *testing.T) {
	pFree := databases.GetTierPolicy("Free")
	if pFree.MaxGroups != 30 || pFree.MaxLists != 1 || pFree.DailySendLimit != 50 || pFree.AllowManualJeda || pFree.AllowLicenseToken {
		t.Fatalf("unexpected Free policy: %+v", pFree)
	}
	pPro := databases.GetTierPolicy("Pro")
	if pPro.MaxGroups != 0 || !pPro.AllowManualJeda || !pPro.AllowLicenseToken || pPro.AllowCustomWatermark || pPro.AllowForwardMode {
		t.Fatalf("unexpected Pro policy: %+v", pPro)
	}
	pProPlus := databases.GetTierPolicy("Pro++")
	if !pProPlus.AllowCustomWatermark || !pProPlus.AllowForwardMode || !pProPlus.AllowTopic || !pProPlus.AllowExpertExtra {
		t.Fatalf("unexpected Pro++ policy: %+v", pProPlus)
	}

	dbPath := filepath.Join(t.TempDir(), "tier_priority.db")
	sq, err := databases.NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	defer sq.DB.Close()

	ctx := context.Background()
	const uid int64 = 10001
	freeLic := &databases.UserLicense{
		TokenSHA:    ComputeTokenSHA(GenerateMasterToken(), uid),
		MasterToken: GenerateMasterToken(),
		OwnerChatID: uid,
		UserbotID:   uid,
		Tier:        "Free",
		ExpiresAt:   time.Now().Add(30 * 24 * time.Hour),
		CreatedAt:   time.Now(),
		Status:      "active",
	}
	proPlusLic := &databases.UserLicense{
		TokenSHA:           ComputeTokenSHA(GenerateMasterToken(), uid),
		MasterToken:        GenerateMasterToken(),
		OwnerChatID:        uid,
		UserbotID:          uid,
		Tier:               "Pro++",
		CanCustomWatermark: true,
		ExpiresAt:          time.Now().Add(7 * 24 * time.Hour),
		CreatedAt:          time.Now(),
		Status:             "active",
	}
	_ = sq.SaveUserLicense(ctx, freeLic)
	_ = sq.SaveUserLicense(ctx, proPlusLic)

	active, err := sq.GetActiveUserLicense(ctx, uid)
	if err != nil || active == nil || active.Tier != "Pro++" {
		t.Fatalf("expected Pro++ (7d) to beat Free (30d), got %+v (err=%v)", active, err)
	}
}

func TestMultiTokenSlotIsolationSwitchTransferAndExpiryFallback(t *testing.T) {
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "token_slots.db")
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
	const userA int64 = 2001
	const userB int64 = 2002

	freeMaster := GenerateMasterToken()
	_ = dm.SaveTokenMaster(ctx, &databases.TokenMaster{
		Token:           freeMaster,
		Tier:            "Free",
		DurationSeconds: 7 * 86400,
		IsShared:        true,
		CreatedAt:       time.Now(),
		Status:          "active",
	})

	licFree, _, errMsg := h.applyVoucherUpgrade(ctx, userA, freeMaster)
	if errMsg != "" || licFree == nil || licFree.Tier != "Free" {
		t.Fatalf("Free activation failed: lic=%+v err=%q", licFree, errMsg)
	}
	cfgFree := dm.GetUserConfig(ctx, userA)
	cfgFree.Lists = []int{101}
	cfgFree.Delay = 12
	_ = dm.SaveUserConfig(ctx, cfgFree)

	proPlusMaster := GenerateMasterToken()
	_ = dm.SaveTokenMaster(ctx, &databases.TokenMaster{
		Token:           proPlusMaster,
		Tier:            "Pro++",
		DurationSeconds: 30 * 86400,
		CreatedAt:       time.Now(),
		Status:          "active",
	})
	licProPlus, _, errMsg := h.applyVoucherUpgrade(ctx, userA, proPlusMaster)
	if errMsg != "" || licProPlus == nil || licProPlus.Tier != "Pro++" || !licProPlus.CanCustomWatermark {
		t.Fatalf("Pro++ slot creation failed: lic=%+v err=%q", licProPlus, errMsg)
	}
	if licProPlus.TokenSHA == licFree.TokenSHA {
		t.Fatalf("expected distinct TokenSHA for new Pro++ slot")
	}

	cfgProPlus := dm.GetUserConfig(ctx, userA)
	if cfgProPlus.ActiveTokenSHA != licProPlus.TokenSHA || len(cfgProPlus.Lists) != 0 {
		t.Fatalf("expected fresh empty vault for Pro++, got sha=%q lists=%v", cfgProPlus.ActiveTokenSHA, cfgProPlus.Lists)
	}
	cfgProPlus.Lists = []int{201, 202}
	cfgProPlus.Groups = make([]databases.GroupEntry, 40)
	for i := range cfgProPlus.Groups {
		cfgProPlus.Groups[i] = databases.GroupEntry{Target: fmt.Sprintf("@propp%d", i+1), ChatID: int64(500 + i)}
	}
	cfgProPlus.TopicEnabled = true
	cfgProPlus.Topics = []databases.TopicEntry{{ChatID: 500, TopicID: 2, Title: "Promo"}}
	cfgProPlus.Delay = 10
	cfgProPlus.Timer = "08:00 - 22:00"
	cfgProPlus.Watermark = "PRO_WM"
	cfgProPlus.Admin = "777"
	cfgProPlus.Email = "proplus@example.com"
	_ = dm.SaveUserConfig(ctx, cfgProPlus)

	proMaster := GenerateMasterToken()
	_ = dm.SaveTokenMaster(ctx, &databases.TokenMaster{
		Token:           proMaster,
		Tier:            "Pro",
		DurationSeconds: 30 * 86400,
		CreatedAt:       time.Now(),
		Status:          "active",
	})
	licPro, _, errMsg := h.applyVoucherUpgrade(ctx, userA, proMaster)
	if errMsg != "" || licPro == nil || licPro.Tier != "Pro" {
		t.Fatalf("adding Pro slot after Pro++ should succeed, got lic=%+v err=%q", licPro, errMsg)
	}
	if _, _, userBErr := h.applyVoucherUpgrade(ctx, userB, proMaster); userBErr == "" {
		t.Fatalf("expected used personal voucher proMaster to be rejected for User B")
	}

	activeSlots, _ := dm.ListActiveUserLicenses(ctx, userA)
	if len(activeSlots) != 3 {
		t.Fatalf("expected 3 independent active slots (Pro++, Pro, Free), got %d", len(activeSlots))
	}
	for _, slot := range activeSlots {
		cbData := "panel:token:switch:" + shortTokenSwitchKey(slot.TokenSHA)
		if len(cbData) > 64 {
			t.Fatalf("callback_data exceeds Telegram 64-byte limit: len=%d data=%q", len(cbData), cbData)
		}
	}

	curActive, _ := dm.GetActiveUserLicense(ctx, userA)
	if curActive == nil || curActive.TokenSHA != licPro.TokenSHA || curActive.Tier != "Pro" {
		t.Fatalf("expected newly added Pro slot to be immediately active, got %+v", curActive)
	}
	cfgPro := dm.GetUserConfig(ctx, userA)
	if len(cfgPro.Lists) != 0 || cfgPro.Watermark != "" || cfgPro.Timer != "Belum diatur" {
		t.Fatalf("expected clean isolated vault for Pro, got lists=%v wm=%q timer=%q", cfgPro.Lists, cfgPro.Watermark, cfgPro.Timer)
	}
	cfgPro.Lists = []int{301, 302}
	cfgPro.Delay = 25
	cfgPro.Email = "pro@example.com"
	_ = dm.SaveUserConfig(ctx, cfgPro)

	swFree, _, errMsg := h.applyVoucherUpgrade(ctx, userA, freeMaster)
	if errMsg != "" || swFree == nil || swFree.TokenSHA != licFree.TokenSHA {
		t.Fatalf("expected re-entering shared Free token to switch to existing Free slot, got lic=%+v err=%q", swFree, errMsg)
	}
	slotsAfterFree, _ := dm.ListActiveUserLicenses(ctx, userA)
	if len(slotsAfterFree) != 3 {
		t.Fatalf("expected Free re-entry not to create duplicate slot, got %d", len(slotsAfterFree))
	}
	cfgCheckFree := dm.GetUserConfig(ctx, userA)
	if len(cfgCheckFree.Lists) != 1 || cfgCheckFree.Lists[0] != 101 || cfgCheckFree.Delay != 12 {
		t.Fatalf("expected Free vault restored [101], delay=12; got lists=%v delay=%d", cfgCheckFree.Lists, cfgCheckFree.Delay)
	}

	h.ShowSwitchTokenMenu(ctx, b, userA, userA)
	h.PanelCallback(ctx, b, &botgotd.Update{
		CallbackQuery: &botgotd.CallbackQuery{
			ID:   "cb_sw_propp",
			From: botgotd.User{ID: userA},
			Message: &botgotd.MaybeInaccessibleMessage{
				Message: &botgotd.Message{ID: 88, Chat: botgotd.Chat{ID: userA}},
			},
			Data: "panel:token:switch:" + shortTokenSwitchKey(licProPlus.TokenSHA),
		},
	})
	_ = rpcLog
	restoredProPlus := dm.GetUserConfig(ctx, userA)
	if restoredProPlus.ActiveTokenSHA != licProPlus.TokenSHA || len(restoredProPlus.Lists) != 2 || len(restoredProPlus.Groups) != 40 || restoredProPlus.Watermark != "PRO_WM" || restoredProPlus.Timer != "08:00 - 22:00" || restoredProPlus.Email != "proplus@example.com" {
		t.Fatalf("expected full Pro++ vault restored on switch, got %+v", restoredProPlus)
	}

	h.PromptUpgradeToken(ctx, b, userA, userA)
	if !h.HandlePanelInput(ctx, b, userA, userA, licPro.TokenSHA, nil) {
		t.Fatalf("expected owned GOTD_SHA input in waiting_upgrade_token to be handled")
	}
	if got := dm.GetUserConfig(ctx, userA); got.ActiveTokenSHA != licPro.TokenSHA || len(got.Lists) != 2 || got.Lists[0] != 301 || got.Delay != 25 {
		t.Fatalf("expected owned GOTD_SHA switch to Pro vault, got %+v", got)
	}

	newLicB, txErr := h.transferTokenSlotBetweenAccounts(ctx, userA, userB, userB, licPro)
	if txErr != nil || newLicB == nil || newLicB.TokenSHA == licPro.TokenSHA {
		t.Fatalf("selective slot transfer failed: newLic=%+v err=%v", newLicB, txErr)
	}
	if oldPro, _ := dm.GetUserLicenseBySHA(ctx, licPro.TokenSHA); oldPro == nil || oldPro.Status != "transferred" {
		t.Fatalf("expected old Pro slot marked transferred, got %+v", oldPro)
	}
	cfgB := dm.GetUserConfig(ctx, userB)
	if cfgB.ActiveTokenSHA != newLicB.TokenSHA || len(cfgB.Lists) != 2 || cfgB.Lists[0] != 301 || cfgB.Delay != 25 || cfgB.Email != "pro@example.com" {
		t.Fatalf("expected userB to receive Pro vault after selective transfer, got %+v", cfgB)
	}
	cfgAAfterTx := dm.GetUserConfig(ctx, userA)
	if cfgAAfterTx.ActiveTokenSHA != licProPlus.TokenSHA || len(cfgAAfterTx.Groups) != 40 {
		t.Fatalf("expected userA to auto-fallback to remaining Pro++ slot cleanly, got sha=%q groups=%d", cfgAAfterTx.ActiveTokenSHA, len(cfgAAfterTx.Groups))
	}

	h.expireSingleLicense(ctx, b, licProPlus)
	cfgAfterExp1 := dm.GetUserConfig(ctx, userA)
	if cfgAfterExp1.ActiveTokenSHA != licFree.TokenSHA || len(cfgAfterExp1.Lists) != 1 || cfgAfterExp1.Lists[0] != 101 {
		t.Fatalf("expected expiry fallback to remaining Free slot with [101], got sha=%q lists=%v", cfgAfterExp1.ActiveTokenSHA, cfgAfterExp1.Lists)
	}

	h.expireSingleLicense(ctx, b, licFree)
	if noneLeft, _ := dm.GetActiveUserLicense(ctx, userA); noneLeft != nil {
		t.Fatalf("expected no active licenses after last slot expired, got %+v", noneLeft)
	}
	cfgAfterExp2 := dm.GetUserConfig(ctx, userA)
	if cfgAfterExp2.ActiveTokenSHA != "" || cfgAfterExp2.Status != "Berhenti" {
		t.Fatalf("expected stopped state with empty ActiveTokenSHA after last slot expiry, got sha=%q status=%q", cfgAfterExp2.ActiveTokenSHA, cfgAfterExp2.Status)
	}
}

func TestOwnerTokenManageAndDataBackupRestoreAndTransfer(t *testing.T) {
	t.Setenv("OWNER", "9999")
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "owner_manage.db")
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
	const userA int64 = 3001
	const userB int64 = 3002

	h.handleOwnerTokenManage(ctx, b, 9999, 9999, []string{"/token", "upgrade", "3001", "pro++", "10h"})
	licA, _ := dm.GetActiveUserLicense(ctx, userA)
	if licA == nil || licA.Tier != "Pro++" || !licA.CanCustomWatermark {
		t.Fatalf("expected Owner upgrade to activate Pro++ for userA, got %+v", licA)
	}

	beforeExp := licA.ExpiresAt
	h.handleOwnerTokenManage(ctx, b, 9999, 9999, []string{"/token", "extend", "3001", "5h"})
	licExt, _ := dm.GetActiveUserLicense(ctx, userA)
	if licExt == nil || licExt.ExpiresAt.Sub(beforeExp) < 4*24*time.Hour {
		t.Fatalf("expected Owner extend +5h, got %v -> %v", beforeExp, licExt.ExpiresAt)
	}

	h.handleOwnerTokenManage(ctx, b, 9999, 9999, []string{"/token", "info", "3001"})

	cfgA := dm.GetUserConfig(ctx, userA)
	cfgA.Lists = []int{11, 22, 33}
	cfgA.Groups = make([]databases.GroupEntry, 35)
	for i := range cfgA.Groups {
		cfgA.Groups[i] = databases.GroupEntry{Target: fmt.Sprintf("@grp%d", i+1), ChatID: int64(100 + i)}
	}
	cfgA.Admin = "4001"
	cfgA.Email = "cadangan@example.com"
	_ = dm.SaveUserConfig(ctx, cfgA)

	h.dispatchSessionRevokedBackup(b, userA, "AUTH_KEY_UNREGISTERED")
	foundAdminAlert := false
	var sentCode string
	for _, call := range rpcLog.getRequests() {
		if (strings.HasSuffix(call.Path, "/sendMessage") || strings.HasSuffix(call.Path, "/sendRichMessage")) && fmt.Sprintf("%v", call.Body["chat_id"]) == "4001" {
			foundAdminAlert = true
			txt, _ := call.Body["text"].(string)
			if txt == "" {
				if rm, ok := call.Body["rich_message"].(map[string]any); ok {
					txt, _ = rm["html"].(string)
				}
			}
			if idx := strings.Index(txt, "GOTD:B"); idx >= 0 {
				if end := strings.IndexAny(txt[idx:], "<\n "); end > 0 {
					sentCode = txt[idx : idx+end]
				} else {
					sentCode = txt[idx:]
				}
			} else if idx := strings.Index(txt, "GOTD_BKP:"); idx >= 0 {
				if end := strings.IndexAny(txt[idx:], "<\n "); end > 0 {
					sentCode = txt[idx : idx+end]
				} else {
					sentCode = txt[idx:]
				}
			}
		}
	}
	if !foundAdminAlert || sentCode == "" {
		t.Fatalf("expected emergency backup alert to admin 4001 with GOTD:B, found=%v code=%q", foundAdminAlert, sentCode)
	}

	_ = dm.SaveUserLicense(ctx, &databases.UserLicense{
		TokenSHA:    ComputeTokenSHA(GenerateMasterToken(), userB),
		MasterToken: GenerateMasterToken(),
		OwnerChatID: userB,
		UserbotID:   userB,
		Tier:        "Free",
		ExpiresAt:   time.Now().Add(7 * 24 * time.Hour),
		CreatedAt:   time.Now(),
		Status:      "active",
	})

	h.HandleAmbilToken(ctx, b, userB, userB)
	h.PromptJedaPerGrup(ctx, b, userB, userB)
	if h.getPanelState(userB) != "" {
		t.Fatalf("expected Free tier to deny JedaPerGrup without entering waiting state")
	}
	h.PromptRestoreDataBackup(ctx, b, userB)
	if !h.HandlePanelInput(ctx, b, userB, userB, sentCode, nil) {
		t.Fatalf("expected restore backup code to be handled")
	}
	cfgB := dm.GetUserConfig(ctx, userB)
	if len(cfgB.Lists) != 1 || len(cfgB.Groups) != 30 {
		t.Fatalf("expected restored data on Free to be capped (1 list, 30 groups), got (%d lists, %d groups)", len(cfgB.Lists), len(cfgB.Groups))
	}

	_ = dm.GetUserConfig(ctx, 3003)
	if err := dm.RelinkUserData(ctx, userA, 3003); err != nil {
		t.Fatalf("RelinkUserData failed when target already has default config: %v", err)
	}
	cfgNew := dm.GetUserConfig(ctx, 3003)
	if len(cfgNew.Groups) != 35 {
		t.Fatalf("expected relinked config to preserve 35 groups, got %d", len(cfgNew.Groups))
	}
	_ = dm.MarkUserLicenseStatus(ctx, licExt.TokenSHA, "transferred")
	if got, _ := dm.GetUserLicenseBySHA(ctx, licExt.TokenSHA); got == nil || got.Status != "transferred" {
		t.Fatalf("expected old SHA status 'transferred', got %+v", got)
	}

	h.handleOwnerTokenManage(ctx, b, 9999, 9999, []string{"/token", "revoke", "3003"})
	if activeAfterRevoke, _ := dm.GetActiveUserLicense(ctx, 3003); activeAfterRevoke != nil {
		t.Fatalf("expected no active license after revoke, got %+v", activeAfterRevoke)
	}
	_ = assets.SettingsMenuMarkup
	_ = (&botgotd.Bot{})
}
