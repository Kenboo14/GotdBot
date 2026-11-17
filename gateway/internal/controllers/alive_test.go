package controllers

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"assterisk/gateway/internal/databases"
)

func TestFormatAliveExpiredTime_UTC7(t *testing.T) {
	utcTime := time.Date(2028, time.July, 19, 16, 54, 0, 0, time.UTC)
	got := formatAliveExpiredTime(utcTime)
	want := "23:54, 19 July 2028"
	if got != want {
		t.Fatalf("formatAliveExpiredTime() = %q, want %q", got, want)
	}
}

func TestFormatServerUptimeAlive(t *testing.T) {
	cases := []struct {
		dur  time.Duration
		want string
	}{
		{7*24*time.Hour + 14*time.Hour + 20*time.Minute, "7 hari, 14 jam, 20 menit"},
		{14*time.Hour + 20*time.Minute, "14 jam, 20 menit"},
		{20 * time.Minute, "20 menit"},
		{45 * time.Second, "45 detik"},
	}
	for _, tc := range cases {
		if got := formatServerUptimeAlive(tc.dur); got != tc.want {
			t.Fatalf("formatServerUptimeAlive(%v) = %q, want %q", tc.dur, got, tc.want)
		}
	}
}

func TestBuildAliveMessageAndKeyboard(t *testing.T) {
	text := buildAliveMessageText(5637241014, "23:54, 19 July 2028", "7 hari, 14 jam, 20 menit")
	for _, want := range []string{
		"── <b><i>GO UserBot</i></b> ──",
		"<b>UserID:</b> <code>5637241014</code>",
		"<b>Expired:</b> <i>23:54, 19 July 2028</i>",
		"<b>Uptime:</b> <i>7 hari, 14 jam, 20 menit</i>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("alive text missing %q:\n%s", want, text)
		}
	}

	kb := buildAliveKeyboard(5637241014, 111222)
	if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 1 {
		t.Fatalf("unexpected alive keyboard layout: %+v", kb)
	}
	btn := kb.InlineKeyboard[0][0]
	if btn.Text != "Ping!" || btn.CallbackData != "alive_ping:5637241014:111222" {
		t.Fatalf("unexpected Ping button: %+v", btn)
	}
}

func TestResolveAliveExpired_FromSQLiteLicense(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_alive.db")
	dm, err := databases.NewDataManager(ctx, ctx, "", "", dbPath)
	if err != nil {
		t.Fatalf("NewDataManager failed: %v", err)
	}
	defer dm.Close()

	expUTC := time.Date(2028, time.July, 19, 16, 54, 0, 0, time.UTC)
	if err := dm.SaveUserLicense(ctx, &databases.UserLicense{
		TokenSHA:    "GOTD_SHA:alive123",
		MasterToken: "GOTD:alive123",
		OwnerChatID: 5637241014,
		UserbotID:   5637241014,
		Tier:        "Pro",
		ExpiresAt:   expUTC,
		CreatedAt:   time.Now(),
		Status:      "active",
	}); err != nil {
		t.Fatalf("SaveUserLicense failed: %v", err)
	}

	got := resolveAliveExpired(ctx, dm, 5637241014, 5637241014)
	if got != "23:54, 19 July 2028" {
		t.Fatalf("resolveAliveExpired() = %q, want %q", got, "23:54, 19 July 2028")
	}
}

func TestFormatAlivePingAlert(t *testing.T) {
	if got := formatAlivePingAlert(alivePingResult{OK: true, MS: 24.089}); got != "Pong..!!\n24.089ms" {
		t.Fatalf("unexpected ping alert: %q", got)
	}
	if got := formatAlivePingAlert(alivePingResult{OK: false}); got != "Userbot sedang offline atau tidak aktif." {
		t.Fatalf("unexpected offline alert: %q", got)
	}
}
