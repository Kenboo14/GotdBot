package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"context"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFernetTokensBinaryLayoutTamperAndCallbackLen(t *testing.T) {
	t.Setenv("TOKEN_FERNET_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")

	mTok := GenerateMasterToken()
	if !strings.HasPrefix(mTok, "GOTD:M") {
		t.Fatalf("expected GOTD:M prefix, got %q", mTok)
	}
	doublePartM := strings.TrimPrefix(mTok, "GOTD:M")
	if len(doublePartM) != 134 {
		t.Fatalf("expected double-base64 payload length 134, got %d (%q)", len(doublePartM), doublePartM)
	}
	innerBytesM, err := decodeBase64Any(doublePartM)
	if err != nil {
		t.Fatalf("outer base64 decode: %v", err)
	}
	rawFernetM, err := decodeBase64Any(string(innerBytesM))
	if err != nil {
		t.Fatalf("inner base64 decode: %v", err)
	}
	if len(rawFernetM) != 73 || rawFernetM[0] != 0x80 {
		t.Fatalf("unexpected raw Fernet layout: len=%d version=0x%x", len(rawFernetM), rawFernetM[0])
	}

	sTok1 := GenerateSessionToken(mTok, 987654321)
	sTok2 := GenerateSessionToken(mTok, 987654321)
	if len(strings.TrimPrefix(sTok1, "GOTD:S")) != 134 {
		t.Fatalf("expected session double-base64 len 134, got %d", len(strings.TrimPrefix(sTok1, "GOTD:S")))
	}
	if shortTokenSwitchKey(sTok1) == shortTokenSwitchKey(sTok2) {
		t.Fatalf("expected distinct switch keys for two GOTD:S tokens")
	}
	if cb := "panel:token:switch:" + shortTokenSwitchKey(sTok1); len(cb) > 64 {
		t.Fatalf("switch callback_data too long: %d", len(cb))
	}
	if cb := "token_set_duration:apply:" + tokenCallbackKey(mTok) + ":30d"; len(cb) > 64 {
		t.Fatalf("master callback_data too long: %d", len(cb))
	}

	rawRunes := []rune(sTok1)
	lastIdx := len(rawRunes) - 1
	if rawRunes[lastIdx] == 'A' {
		rawRunes[lastIdx] = 'B'
	} else {
		rawRunes[lastIdx] = 'A'
	}
	if _, _, decErr := DecodeFernetSessionToken(string(rawRunes)); decErr == nil {
		t.Fatalf("expected tampered GOTD:S token to fail HMAC verification")
	}

	t.Setenv("TOKEN_FERNET_KEY", "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=")
	if _, decErr := DecodeFernetMasterToken(mTok); decErr == nil {
		t.Fatalf("expected wrong TOKEN_FERNET_KEY to fail master decode")
	}
}

func TestPortableBackupInlineLargeFileDocumentRestoreAndExpiryGuard(t *testing.T) {
	t.Setenv("TOKEN_FERNET_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")

	var docBytesUploaded []byte
	var uploadedFileName string
	var backupDownloadContent string

	server, b, rpcLog := setupMockTelegramServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/sendDocument") {
			if err := r.ParseMultipartForm(4 << 20); err == nil && r.MultipartForm != nil {
				if files := r.MultipartForm.File["document"]; len(files) > 0 {
					uploadedFileName = files[0].Filename
					if f, err := files[0].Open(); err == nil {
						buf := make([]byte, 128<<10)
						n, _ := f.Read(buf)
						docBytesUploaded = buf[:n]
						_ = f.Close()
					}
				}
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":999,"chat":{"id":1,"type":"private"}}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/getFile") {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"doc_123","file_path":"documents/backup.txt"}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/documents/backup.txt") {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(backupDownloadContent))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	})
	defer server.Close()

	ctx := context.Background()
	dm, err := databases.NewDataManager(ctx, ctx, "", "", filepath.Join(t.TempDir(), "fernet_bkp.db"))
	if err != nil {
		t.Fatalf("NewDataManager: %v", err)
	}
	defer func() {
		if sq := dm.GetSQLiteDB(); sq != nil {
			_ = sq.Close()
		}
	}()
	h := NewHandler(nil, dm)

	const srcUser int64 = 5001
	const dstFreeUser int64 = 5002

	srcExp := time.Now().Add(365 * 24 * time.Hour)
	_ = dm.SaveUserLicense(ctx, &databases.UserLicense{
		TokenSHA:           ComputeTokenSHA("M", srcUser),
		MasterToken:        GenerateMasterToken(),
		OwnerChatID:        srcUser,
		UserbotID:          srcUser,
		Tier:               "Pro++",
		CanCustomWatermark: true,
		ExpiresAt:          srcExp,
		CreatedAt:          time.Now(),
		Status:             "active",
	})
	dstExp := time.Now().Add(48 * time.Hour)
	_ = dm.SaveUserLicense(ctx, &databases.UserLicense{
		TokenSHA:    ComputeTokenSHA("M2", dstFreeUser),
		MasterToken: GenerateMasterToken(),
		OwnerChatID: dstFreeUser,
		UserbotID:   dstFreeUser,
		Tier:        "Free",
		ExpiresAt:   dstExp,
		CreatedAt:   time.Now(),
		Status:      "active",
	})

	cfgSmall := dm.GetUserConfig(ctx, srcUser)
	cfgSmall.Lists = []int{11, 22, 33}
	cfgSmall.Groups = []databases.GroupEntry{
		{Target: "-1001234567890", Title: "Lapak Satu", ChatID: -1001234567890, PeerType: "channel", IsForum: true},
		{Target: "-987654", Title: "Grup Basic", ChatID: -987654, PeerType: "chat"},
	}
	cfgSmall.Topics = []databases.TopicEntry{{ChatID: -1001234567890, TopicID: 7, Title: "Promo"}}
	cfgSmall.Delay = 12
	cfgSmall.Mode = "Forward"
	cfgSmall.Watermark = "PRO_WM_SECRET"
	cfgSmall.Timer = "09:00 - 21:00"
	cfgSmall.Safemode = true
	_ = dm.SaveUserConfig(ctx, cfgSmall)

	smallCode, err := EncodePortableBackup(cfgSmall, nil)
	if err != nil || !strings.HasPrefix(smallCode, "GOTD:B") {
		t.Fatalf("EncodePortableBackup small failed: %v code=%q", err, smallCode)
	}
	h.createAndRenderDataBackup(ctx, b, srcUser, srcUser, 0)
	if rpcLog.countByMethod("sendDocument") != 0 {
		t.Fatalf("expected inline backup when <= 4000 chars, not sendDocument")
	}

	h.PromptRestoreDataBackup(ctx, b, dstFreeUser)
	if !h.HandlePanelInput(ctx, b, dstFreeUser, dstFreeUser, smallCode, nil) {
		t.Fatalf("expected GOTD:B inline restore to be handled")
	}

	restoredFree := dm.GetUserConfig(ctx, dstFreeUser)
	if len(restoredFree.Lists) != 1 || len(restoredFree.Groups) != 2 || len(restoredFree.Topics) != 0 || restoredFree.Watermark != "" || restoredFree.DelayMode != "aman" || restoredFree.Mode != "Normal" || restoredFree.Timer != "09:00 - 21:00" || !restoredFree.Safemode {
		t.Fatalf("unexpected Free restore enforcement result: %+v", restoredFree)
	}
	dstLicAfter, _ := dm.GetActiveUserLicense(ctx, dstFreeUser)
	if dstLicAfter == nil || dstLicAfter.Tier != "Free" || dstLicAfter.ExpiresAt.Sub(dstExp).Abs() > time.Second {
		t.Fatalf("restore must NEVER mutate receiver Tier or ExpiresAt: got %+v (want %v)", dstLicAfter, dstExp)
	}

	cfgLarge := dm.GetUserConfig(ctx, srcUser)
	cfgLarge.Groups = make([]databases.GroupEntry, 260)
	for i := range cfgLarge.Groups {
		id := -int64(1001000000000 + i)
		cfgLarge.Groups[i] = databases.GroupEntry{
			Target:   strconv.FormatInt(id, 10),
			Title:    "Supergroup Jual Beli Komunitas Nusantara Sangat Panjang",
			ChatID:   id,
			PeerType: "channel",
		}
	}
	_ = dm.SaveUserConfig(ctx, cfgLarge)
	largeCode, err := EncodePortableBackup(cfgLarge, nil)
	if err != nil || len(largeCode) <= maxInlineBackupCodeLen {
		t.Fatalf("expected largeCode > %d chars, got %d (err=%v)", maxInlineBackupCodeLen, len(largeCode), err)
	}
	h.createAndRenderDataBackup(ctx, b, srcUser, srcUser, 0)
	if rpcLog.countByMethod("sendDocument") != 1 || len(docBytesUploaded) == 0 || !strings.HasSuffix(uploadedFileName, ".txt") {
		t.Fatalf("expected >4000 char backup to be uploaded as .txt document, count=%d name=%q len=%d", rpcLog.countByMethod("sendDocument"), uploadedFileName, len(docBytesUploaded))
	}

	backupDownloadContent = string(docBytesUploaded)
	const dstProUser int64 = 5003
	_ = dm.SaveUserLicense(ctx, &databases.UserLicense{
		TokenSHA:           ComputeTokenSHA("M3", dstProUser),
		MasterToken:        GenerateMasterToken(),
		OwnerChatID:        dstProUser,
		UserbotID:          dstProUser,
		Tier:               "Pro++",
		CanCustomWatermark: true,
		ExpiresAt:          time.Now().Add(72 * time.Hour),
		CreatedAt:          time.Now(),
		Status:             "active",
	})
	h.PromptRestoreDataBackup(ctx, b, dstProUser)
	h.HandlePanelMedia(ctx, b, &botgotd.Update{
		Message: &botgotd.Message{
			ID:   888,
			From: &botgotd.User{ID: dstProUser},
			Chat: botgotd.Chat{ID: dstProUser},
			Document: &botgotd.Document{
				FileID:   "doc_123",
				FileName: uploadedFileName,
				MimeType: "text/plain",
				FileSize: int64(len(docBytesUploaded)),
			},
		},
	})
	if h.getPanelState(dstProUser) != "" {
		t.Fatalf("expected panel state cleared after successful document restore")
	}
	restoredPro := dm.GetUserConfig(ctx, dstProUser)
	if len(restoredPro.Groups) != 260 || restoredPro.Watermark != "PRO_WM_SECRET" || len(restoredPro.Topics) != 1 {
		t.Fatalf("expected Pro++ document restore with 260 groups, watermark, and topic; got groups=%d wm=%q topics=%d", len(restoredPro.Groups), restoredPro.Watermark, len(restoredPro.Topics))
	}
}

func TestBackwardCompatibilityLegacyTokensAndBackup(t *testing.T) {
	t.Setenv("TOKEN_FERNET_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	ctx := context.Background()
	dm, err := databases.NewDataManager(ctx, ctx, "", "", filepath.Join(t.TempDir(), "legacy_compat.db"))
	if err != nil {
		t.Fatalf("NewDataManager: %v", err)
	}
	defer func() {
		if sq := dm.GetSQLiteDB(); sq != nil {
			_ = sq.Close()
		}
	}()
	h := NewHandler(nil, dm)
	const uid int64 = 6001
	const ownerID int64 = 9999
	t.Setenv("OWNER", "9999")

	legacyMaster := "GOTD:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef"
	_ = dm.SaveTokenMaster(ctx, &databases.TokenMaster{
		Token:           legacyMaster,
		Tier:            "Pro",
		DurationSeconds: 10 * 86400,
		CreatedBy:       ownerID,
		CreatedAt:       time.Now(),
		Status:          "active",
	})
	lic, _, errMsg := h.applyVoucherUpgrade(ctx, uid, legacyMaster)
	if errMsg != "" || lic == nil || !strings.HasPrefix(lic.TokenSHA, "GOTD:S") {
		t.Fatalf("expected legacy master voucher upgrade to issue GOTD:S slot, got lic=%+v err=%q", lic, errMsg)
	}

	gotByMaster, errMaster := dm.GetUserLicenseByOwnerAndMaster(ctx, uid, legacyMaster)
	if errMaster != nil || gotByMaster == nil || gotByMaster.TokenSHA != lic.TokenSHA {
		t.Fatalf("expected GetUserLicenseByOwnerAndMaster to return claimed slot, got %+v (err=%v)", gotByMaster, errMaster)
	}

	sharedMaster := GenerateMasterToken()
	_ = dm.SaveTokenMaster(ctx, &databases.TokenMaster{
		Token:           sharedMaster,
		Tier:            "Pro",
		DurationSeconds: 10 * 86400,
		IsShared:        true,
		CreatedBy:       ownerID,
		CreatedAt:       time.Now(),
		Status:          "active",
	})
	licShared, _, errShared := h.applyVoucherUpgrade(ctx, uid, sharedMaster)
	if errShared != "" || licShared == nil {
		t.Fatalf("expected shared GOTD:M voucher activation to succeed, got err=%q", errShared)
	}
	licSecondTry, labelSecond, errSecond := h.applyVoucherUpgrade(ctx, uid, sharedMaster)
	if errSecond != "" || licSecondTry == nil || licSecondTry.TokenSHA != licShared.TokenSHA || !strings.Contains(labelSecond, "Beralih") {
		t.Fatalf("expected re-applying active shared voucher to switch existing slot via GetUserLicenseByOwnerAndMaster, got lic=%+v label=%q err=%q", licSecondTry, labelSecond, errSecond)
	}

	legacySHA := ComputeLegacyTokenSHA("GOTD:ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ", 6002)
	_ = dm.SaveUserLicense(ctx, &databases.UserLicense{
		TokenSHA:    legacySHA,
		MasterToken: "GOTD:ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ",
		OwnerChatID: 6002,
		UserbotID:   6002,
		Tier:        "Pro",
		ExpiresAt:   time.Now().Add(48 * time.Hour),
		CreatedAt:   time.Now(),
		Status:      "active",
	})
	if foundLic, _ := h.lookupLicenseByQuery(ctx, legacySHA); foundLic == nil || foundLic.OwnerChatID != 6002 {
		t.Fatalf("expected legacy GOTD_SHA lookup to succeed, got %+v", foundLic)
	}
	if foundNew, _ := h.lookupLicenseByQuery(ctx, lic.TokenSHA); foundNew == nil || foundNew.OwnerChatID != uid {
		t.Fatalf("expected new GOTD:S lookup to succeed, got %+v", foundNew)
	}

	legacyCode := GenerateBackupCode()
	_ = dm.SaveDataBackup(ctx, &databases.DataBackupCode{
		Code:       legacyCode,
		OwnerID:    uid,
		Lists:      []int{901},
		Groups:     []databases.GroupEntry{{Target: "-100444555666", Title: "Legacy", ChatID: -100444555666, PeerType: "channel"}},
		CreatedAt:  time.Now(),
	})
	server, b, _ := setupMockTelegramServer(t, nil)
	defer server.Close()
	h.PromptRestoreDataBackup(ctx, b, uid)
	if !h.HandlePanelInput(ctx, b, uid, uid, legacyCode, nil) {
		t.Fatalf("expected legacy GOTD_BKP code restore to succeed")
	}
	if cfg := dm.GetUserConfig(ctx, uid); len(cfg.Lists) != 1 || cfg.Lists[0] != 901 || len(cfg.Groups) != 1 {
		t.Fatalf("unexpected legacy backup restored state: %+v", cfg)
	}
}

func TestCustomerFacingCopywritingNoTechnicalJargon(t *testing.T) {
	t.Setenv("TOKEN_FERNET_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	server, b, rpcLog := setupMockTelegramServer(t, nil)
	defer server.Close()

	ctx := context.Background()
	dm, err := databases.NewDataManager(ctx, ctx, "", "", filepath.Join(t.TempDir(), "copy_audit.db"))
	if err != nil {
		t.Fatalf("NewDataManager: %v", err)
	}
	defer func() {
		if sq := dm.GetSQLiteDB(); sq != nil {
			_ = sq.Close()
		}
	}()
	h := NewHandler(nil, dm)
	const uid int64 = 7101
	_ = dm.SaveUserLicense(ctx, &databases.UserLicense{
		TokenSHA:           ComputeTokenSHA("M", uid),
		MasterToken:        GenerateMasterToken(),
		OwnerChatID:        uid,
		UserbotID:          uid,
		Tier:               "Pro++",
		CanCustomWatermark: true,
		ExpiresAt:          time.Now().Add(72 * time.Hour),
		CreatedAt:          time.Now(),
		Status:             "active",
	})

	samples := []string{
		assets.TxtInputToken,
		assets.TxtTokenSHANotFound,
		assets.RichInstallSuccess("Pro++", "01-01-2027", "30 Hari", "GOTD:S_SAMPLE"),
	}

	h.ShowSettings(ctx, b, uid, uid)
	h.HandleAmbilToken(ctx, b, uid, uid)
	h.PromptUpgradeToken(ctx, b, uid, uid)
	h.PromptRestoreDataBackup(ctx, b, uid)
	h.HandlePanelInput(ctx, b, uid, uid, "GOTD:S_INVALID", nil)
	h.PromptRestoreDataBackup(ctx, b, uid)
	h.HandlePanelInput(ctx, b, uid, uid, "INVALID_BACKUP", nil)

	h.Sessions.mu.Lock()
	h.Sessions.sessions[uid] = &UserSession{ChatID: uid, State: "waiting_token"}
	h.Sessions.mu.Unlock()
	h.HandleMessage(ctx, b, &botgotd.Update{
		Message: &botgotd.Message{
			ID:   10,
			From: &botgotd.User{ID: uid, FirstName: "Tester"},
			Chat: botgotd.Chat{ID: uid},
			Text: "INVALID_TOKEN",
		},
	})

	samples = append(samples, extractRPCMessages(rpcLog)...)
	forbidden := []string{"sha", "sha-256", "sha256", "gotd_sha", "gotd_bkp", "enkripsi", "terenkripsi", "encrypt", "fernet", "base64", "checksum", "32_huruf"}
	for idx, msg := range samples {
		low := strings.ToLower(msg)
		for _, term := range forbidden {
			if strings.Contains(low, term) {
				t.Fatalf("customer-facing message #%d contains forbidden technical term %q: %s", idx, term, msg)
			}
		}
	}
}

