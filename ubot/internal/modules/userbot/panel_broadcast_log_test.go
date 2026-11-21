package userbot

import (
	"errors"
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
}
