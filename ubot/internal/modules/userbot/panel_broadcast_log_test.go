package userbot

import (
	"assterisk/ubot/internal/databases"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHumanizePanelBroadcastErrorAndNotifFilter(t *testing.T) {
	if !isPanelNotifikasiOff("Nonaktif") || !isPanelNotifikasiOff("Matikan") {
		t.Fatalf("expected Nonaktif/Matikan to disable panel broadcast logs")
	}
	if isPanelNotifikasiOff("Kirim ke Saya") || isPanelNotifikasiOff("Kirim ke saya & Admin") {
		t.Fatalf("did not expect active modes to be treated as off")
	}

	if got := humanizePanelBroadcastError(errors.New("rpcDoRequest: rpc error code 420: SLOWMODE_WAIT_2398")); got != "slowmode aktif, tunggu 2398 detik" {
		t.Fatalf("unexpected slowmode text: %q", got)
	}
	if got := humanizePanelBroadcastError(errors.New("rpcDoRequest: rpc error code 400: CHAT_SEND_PLAIN_FORBIDDEN")); got != "hanya media yang diperbolehkan" {
		t.Fatalf("unexpected send_plain text: %q", got)
	}
	if got := humanizePanelBroadcastError(errors.New("rpcDoRequest: rpc error code 420: FLOOD_WAIT_45")); got != "flood wait, tunggu 45 detik" {
		t.Fatalf("unexpected flood_wait text: %q", got)
	}
}

func TestFormatPanelRoundLogHTMLAndFloodLogHTML(t *testing.T) {
	multi := formatPanelRoundLogHTML("y", "DELAY ALLGROUP [Normal Text]", 1384, 1, []panelRoundFailure{
		{Title: "TEST 3", Reason: "hanya media yang diperbolehkan"},
		{Title: "TESTE", Reason: "slowmode aktif, tunggu 2398 detik"},
	})
	for _, want := range []string{
		"<blockquote>y</blockquote>",
		"<b>DELAY ALLGROUP [Normal Text]</b>",
		"<b>Putaran :</b> <i>1384</i>",
		"<b>Sukses :</b> <i>1</i>",
		"<b>Gagal :</b> <i>2</i>",
		"<b>Alasan :</b>",
		"1. <i>TEST 3 : hanya media yang diperbolehkan</i>",
		"2. <i>TESTE : slowmode aktif, tunggu 2398 detik</i>",
	} {
		if !strings.Contains(multi, want) {
			t.Fatalf("expected %q in multi round log:\n%s", want, multi)
		}
	}

	singleFail := formatPanelRoundLogHTML("oi", "DELAY PERGRUP [Normal Text]", 1125, 0, []panelRoundFailure{
		{Title: "TESTE", Reason: "slowmode aktif, tunggu 3503 detik"},
	})
	for _, want := range []string{
		"<blockquote>oi</blockquote>",
		"<b>DELAY PERGRUP [Normal Text]</b>",
		"<b>Putaran :</b> <i>1125</i>",
		"<b>Gagal :</b> <i>TESTE</i>",
		"<b>Alasan :</b> <i>slowmode aktif, tunggu 3503 detik</i>",
	} {
		if !strings.Contains(singleFail, want) {
			t.Fatalf("expected %q in single failure log:\n%s", want, singleFail)
		}
	}

	floodMsg := formatPanelFloodLogHTML("promo", "DELAY PERGRUP [Normal Text]", "Grup A", 45*time.Second, false, false)
	if !strings.Contains(floodMsg, "FLOOD WAIT TERDETEKSI") || !strings.Contains(floodMsg, "45 detik") || !strings.Contains(floodMsg, "mengirim ulang") {
		t.Fatalf("unexpected flood log HTML:\n%s", floodMsg)
	}

	svc := &Service{}
	snippet, header := svc.resolvePanelPostMeta(t.Context(), nil, nil, jasebConfigDoc{
		Mode:           "Normal",
		DelayMode:      "manual",
		Delay:          0,
		DelayAllGroups: 10,
		Watermark:      "y",
	}, 0)
	if snippet != "y" || header != "DELAY ALLGROUP [Normal Text]" {
		t.Fatalf("unexpected post meta: snippet=%q header=%q", snippet, header)
	}

	_, perGroupHeader := svc.resolvePanelPostMeta(t.Context(), nil, nil, jasebConfigDoc{
		Mode:      "Fast",
		DelayMode: "aman",
		Watermark: "promo",
	}, 0)
	if perGroupHeader != "DELAY PERGRUP [Fast Text]" {
		t.Fatalf("expected DELAY PERGRUP header for aman mode, got %q", perGroupHeader)
	}
}

func TestDelayModeDetectionPerGroupFormatAndApplyNotifikasi(t *testing.T) {
	cases := []struct {
		name string
		cfg  jasebConfigDoc
		want bool
	}{
		{name: "aman", cfg: jasebConfigDoc{DelayMode: "aman"}, want: false},
		{name: "otomatis", cfg: jasebConfigDoc{DelayMode: "otomatis"}, want: false},
		{name: "manual_per_grup", cfg: jasebConfigDoc{DelayMode: "manual", Delay: 15, DelayAllGroups: 0}, want: false},
		{name: "manual_both", cfg: jasebConfigDoc{DelayMode: "manual", Delay: 15, DelayAllGroups: 20}, want: false},
		{name: "manual_all_groups", cfg: jasebConfigDoc{DelayMode: "manual", Delay: 0, DelayAllGroups: 15}, want: true},
		{name: "manual_tanpa_jeda", cfg: jasebConfigDoc{DelayMode: "manual", Delay: 0, DelayAllGroups: 0}, want: true},
	}
	for _, tc := range cases {
		if got := isDelayAllGroupMode(tc.cfg); got != tc.want {
			t.Fatalf("%s: isDelayAllGroupMode=%v, want %v", tc.name, got, tc.want)
		}
	}

	okMsg := formatPanelPerGroupLogHTML("promo A", "DELAY PERGRUP [Normal Text]", 2, "Grup Alpha", true, "")
	for _, want := range []string{
		"<blockquote>promo A</blockquote>",
		"<b>DELAY PERGRUP [Normal Text]</b>",
		"<b>Putaran :</b> <i>2</i>",
		"<b>Sukses :</b> <i>Grup Alpha</i>",
	} {
		if !strings.Contains(okMsg, want) {
			t.Fatalf("expected %q in per-group success log:\n%s", want, okMsg)
		}
	}
	if strings.Contains(okMsg, "Gagal") || strings.Contains(okMsg, "Alasan") {
		t.Fatalf("did not expect failure fields in per-group success log:\n%s", okMsg)
	}

	failMsg := formatPanelPerGroupLogHTML("promo B", "DELAY PERGRUP [Fast Media]", 3, "Grup Beta", false, "slowmode aktif, tunggu 120 detik")
	for _, want := range []string{
		"<blockquote>promo B</blockquote>",
		"<b>DELAY PERGRUP [Fast Media]</b>",
		"<b>Putaran :</b> <i>3</i>",
		"<b>Gagal :</b> <i>Grup Beta</i>",
		"<b>Alasan :</b> <i>slowmode aktif, tunggu 120 detik</i>",
	} {
		if !strings.Contains(failMsg, want) {
			t.Fatalf("expected %q in per-group failure log:\n%s", want, failMsg)
		}
	}

	emptyReasonMsg := formatPanelPerGroupLogHTML("promo C", "DELAY PERGRUP [Normal Text]", 1, "Grup Gamma", false, "")
	if !strings.Contains(emptyReasonMsg, "<b>Alasan :</b> <i>gagal mengirim pesan</i>") {
		t.Fatalf("expected default failure reason fallback, got:\n%s", emptyReasonMsg)
	}

	sq, err := databases.NewSQLiteClient(filepath.Join(t.TempDir(), "ubot_notif.db"))
	if err != nil {
		t.Fatalf("NewSQLiteClient failed: %v", err)
	}
	t.Cleanup(func() {
		if sq != nil && sq.DB != nil {
			_ = sq.DB.Close()
		}
	})

	svc := &Service{
		sqlite:   sq,
		cfgCache: newUbotConfigCache(ubotCfgCacheMaxEntries, ubotCfgCacheTTL),
	}
	const ownerID int64 = 7701
	svc.cfgCache.Set(ownerID, jasebConfigDoc{OwnerID: ownerID, Notifikasi: "Kirim ke Saya"})

	svc.applyPanelNotifikasi(t.Context(), ownerID, "Nonaktif")
	if got := svc.getJasebConfig(t.Context(), ownerID).Notifikasi; got != "Nonaktif" {
		t.Fatalf("expected Nonaktif in ubot config after applyPanelNotifikasi, got %q", got)
	}

	svc.applyPanelNotifikasi(t.Context(), ownerID, "Kirim ke saya & Admin")
	if got := svc.getJasebConfig(t.Context(), ownerID).Notifikasi; got != "Kirim ke saya & Admin" {
		t.Fatalf("expected Kirim ke saya & Admin after second applyPanelNotifikasi, got %q", got)
	}
}
