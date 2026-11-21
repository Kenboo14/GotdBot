package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/models"
	"context"
	"fmt"
	"time"
)

var (
	mainMenuMarkup          = assets.MainMenuMarkup
	statusMenuMarkup        = assets.StatusMenuMarkup
	grupListMenuMarkup      = assets.GrupListMenuMarkup
	tambahGrupOptionsMarkup = assets.TambahGrupOptionsMarkup
	cancelOnlyMarkup        = assets.CancelOnlyMarkup
	aturJedaMarkup          = assets.AturJedaMarkup
	ubahModeMarkup          = assets.UbahModeMarkup
	timerMarkup             = assets.TimerMarkup
	resetMarkup             = assets.ResetMarkup
	hapusListMarkup         = assets.HapusListMarkup
	hapusGrupMarkup         = assets.HapusGrupMarkup
)

func (h *Handler) routePanelAction(ctx context.Context, b *botgotd.Bot, chatID, effectiveUserID int64, effectiveUserName, text string) bool {
	switch text {
	case assets.BtnHome, "/menu":
		h.ShowMainMenu(ctx, b, chatID, effectiveUserID, effectiveUserName)
		return true

	case assets.BtnStatus, "/status":
		h.ShowStatus(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnShop:
		h.ShowShop(ctx, b, chatID)
		return true

	case assets.BtnGrupList:
		h.ShowGrupList(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnExpert:
		if policy, enforced := h.getEffectiveTierPolicy(ctx, effectiveUserID); enforced && !policy.AllowExpertExtra {
			h.denyTierFeature(ctx, b, chatID, policy.Tier, "Expert Extra", "Pro", assets.MainMenuMarkup)
			return true
		}
		h.ShowExpertExtra(ctx, b, chatID)
		return true

	case assets.BtnReset:
		h.PromptReset(ctx, b, chatID)
		return true

	case assets.BtnAturJeda:
		h.PromptAturJeda(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnJedaAman:
		h.applyJedaAman(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnJedaOtomatis:
		h.applyJedaOtomatis(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnJedaPerGrup:
		h.PromptJedaPerGrup(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnJedaPerSemuaGrup:
		h.PromptJedaPerSemuaGrup(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnSkipFloodWait:
		h.PromptSkipFloodWait(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnBantuan:
		h.ShowBantuan(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnSettings:
		h.ShowSettings(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnUpgradeToken:
		h.PromptUpgradeToken(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnGantiToken:
		h.ShowSwitchTokenMenu(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnAmbilToken:
		h.HandleAmbilToken(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnPindahAkses:
		h.HandlePindahAkses(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnBackupData:
		h.HandleCreateDataBackup(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnRestoreData:
		h.PromptRestoreDataBackup(ctx, b, chatID)
		return true

	case assets.BtnFeed:
		h.ShowFeed(ctx, b, chatID)
		return true

	case assets.BtnWebapp:
		h.ShowWebapp(ctx, b, chatID)
		return true

	case assets.BtnCustomWatermark:
		h.PromptWatermark(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnAturEmail:
		h.PromptEmail(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnSafemode:
		h.ToggleSafemode(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnTimer:
		h.PromptTimer(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnUbahMode:
		h.PromptUbahMode(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnAdmin:
		h.PromptAdmin(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnUbahStatus:
		h.ToggleStatus(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnNotifikasi:
		h.ToggleNotifikasi(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnCekList:
		h.DisplayList(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnCekGrup:
		h.DisplayGroup(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnTambahList:
		if policy, enforced := h.getEffectiveTierPolicy(ctx, effectiveUserID); enforced && policy.MaxLists > 0 {
			if _, lCount := h.db.CountGroupsAndLists(ctx, effectiveUserID); lCount >= policy.MaxLists {
				h.denyTierFeature(ctx, b, chatID, policy.Tier, fmt.Sprintf("Tambah List (> %d list)", policy.MaxLists), "Pro", assets.GrupListMenuMarkup)
				return true
			}
		}
		h.PromptTambahList(ctx, b, chatID)
		return true

	case assets.BtnTambahGrup:
		h.PromptTambahGrup(ctx, b, chatID)
		return true

	case assets.BtnTambahkanSemuaGrup:
		h.FetchAllGroupsFromAccount(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnTambahGrupManual:
		h.PromptTambahGrupManual(ctx, b, chatID)
		return true

	case assets.BtnCekTopic:
		if !h.allowTopicFeature(ctx, b, chatID, effectiveUserID) {
			return true
		}
		h.CekTopic(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnTopic:
		if !h.allowTopicFeature(ctx, b, chatID, effectiveUserID) {
			return true
		}
		h.StartTopicMenu(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnListTopic:
		if !h.allowTopicFeature(ctx, b, chatID, effectiveUserID) {
			return true
		}
		h.ShowListTopic(ctx, b, chatID, effectiveUserID, 0)
		return true

	case assets.BtnSebarKeRC:
		h.SebarKeRC(ctx, b, chatID, effectiveUserID)
		return true

	case assets.BtnBatal:
		h.setPanelState(chatID, "")
		h.ShowMainMenu(ctx, b, chatID, effectiveUserID, effectiveUserName)
		return true

	case assets.BtnHapusSemuaGrup:
		_ = h.db.ClearGroups(ctx, effectiveUserID)
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: effectiveUserID, OwnerID: effectiveUserID, Type: "clear_panel_groups"})
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        assets.TxtResetSemuaGrupSukses,
			ParseMode:   "HTML",
			ReplyMarkup: assets.MainMenuMarkup,
		})
		return true

	case assets.BtnHapusSemuaList:
		_ = h.db.ClearLists(ctx, effectiveUserID)
		_ = h.broker.To(ctx, "to:ubot:command", models.PToService{ChatID: chatID, UserID: effectiveUserID, OwnerID: effectiveUserID, Type: "clear_panel_lists"})
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        assets.TxtResetSemuaListSukses,
			ParseMode:   "HTML",
			ReplyMarkup: assets.MainMenuMarkup,
		})
		return true

	case assets.BtnResetKonfigurasi:
		defaultCfg := &databases.UserConfig{
			OwnerID:          effectiveUserID,
			Type:             "Broadcast",
			Delay:            5,
			DelayMode:        "manual",
			DelayAllGroups:   0,
			SkipFloodWait:    false,
			SkipFloodWaitSec: 0,
			Timer:            "Belum diatur",
			Extra:            0,
			Mode:             "Normal",
			Safemode:         false,
			Status:           "Berhenti",
			MasaAktif:        "Belum diatur",
			Notifikasi:       "Kirim ke Saya",
			Email:            "Belum diatur",
			Admin:            "Belum diatur",
			Watermark:        "",
			TopicEnabled:     false,
			TindakanTerkini:  "Siaran otomatis dimatikan",
			TindakanTerakhir: "-",
			UpdatedAt:        time.Now(),
		}
		h.syncConfigMasaAktif(ctx, effectiveUserID, defaultCfg)
		_ = h.db.SaveUserConfig(ctx, defaultCfg)
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        assets.TxtResetKonfigurasiSukses,
			ParseMode:   "HTML",
			ReplyMarkup: assets.MainMenuMarkup,
		})
		return true
	}

	return false
}
