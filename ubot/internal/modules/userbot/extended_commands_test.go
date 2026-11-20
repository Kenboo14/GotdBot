package userbot

import (
	"assterisk/ubot/internal/databases"
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteSudoAndGlobalModeration(t *testing.T) {
	sq, err := databases.NewSQLiteClient(filepath.Join(t.TempDir(), "ext.db"))
	if err != nil {
		t.Fatalf("NewSQLiteClient: %v", err)
	}
	defer sq.Close()

	ctx := context.Background()
	const ownerID int64 = 7701

	if err := sq.AddSudo(ctx, ownerID, 1002); err != nil {
		t.Fatalf("AddSudo 1002: %v", err)
	}
	if err := sq.AddSudo(ctx, ownerID, 1001); err != nil {
		t.Fatalf("AddSudo 1001: %v", err)
	}
	sudoIDs, err := sq.ListSudo(ctx, ownerID)
	if err != nil || len(sudoIDs) != 2 || sudoIDs[0] != 1001 || sudoIDs[1] != 1002 {
		t.Fatalf("unexpected ListSudo: %v err=%v", sudoIDs, err)
	}
	cleared, err := sq.ClearSudo(ctx, ownerID)
	if err != nil || cleared != 2 {
		t.Fatalf("expected 2 cleared sudo rows, got %d err=%v", cleared, err)
	}
	if after, err := sq.ListSudo(ctx, ownerID); err != nil || len(after) != 0 {
		t.Fatalf("expected empty sudo list after ClearSudo, got %v err=%v", after, err)
	}

	if err := sq.AddGlobalMod(ctx, ownerID, "gban", 5001, "Spammer Satu"); err != nil {
		t.Fatalf("AddGlobalMod gban: %v", err)
	}
	if err := sq.AddGlobalMod(ctx, ownerID, "gmute", 5002, "Noisy User"); err != nil {
		t.Fatalf("AddGlobalMod gmute: %v", err)
	}
	isBanned, err := sq.IsGlobalMod(ctx, ownerID, "gban", 5001)
	if err != nil || !isBanned {
		t.Fatalf("expected 5001 gban true, got %v err=%v", isBanned, err)
	}
	gbanRows, err := sq.ListGlobalMod(ctx, ownerID, "gban")
	if err != nil || len(gbanRows) != 1 || gbanRows[0].Label != "Spammer Satu" {
		t.Fatalf("unexpected ListGlobalMod gban: %+v err=%v", gbanRows, err)
	}
	if err := sq.RemoveGlobalMod(ctx, ownerID, "gban", 5001); err != nil {
		t.Fatalf("RemoveGlobalMod: %v", err)
	}
	if still, _ := sq.IsGlobalMod(ctx, ownerID, "gban", 5001); still {
		t.Fatalf("expected 5001 removed from gban")
	}
}

func TestUnicodeFontsMirrorAndJoinTarget(t *testing.T) {
	styles := []string{"bold", "italic", "mono", "script", "double", "smallcaps", "bubble", "square"}
	for _, style := range styles {
		out, ok := applyUnicodeFont(style, "Halo 123")
		if !ok || out == "" || out == "Halo 123" {
			t.Fatalf("expected transformed output for style %q, got %q (ok=%v)", style, out, ok)
		}
	}
	if _, ok := applyUnicodeFont("unknown_style", "abc"); ok {
		t.Fatalf("expected unknown font style to return false")
	}
	if rev := reverseTextRunes("GotdBot 🚀"); rev != "🚀 toBdtoG" {
		t.Fatalf("unexpected reverseTextRunes result: %q", rev)
	}
	if hash, uname := extractJoinTarget("https://t.me/+AbCdEf123?single"); hash != "AbCdEf123" || uname != "" {
		t.Fatalf("expected private invite hash AbCdEf123, got hash=%q uname=%q", hash, uname)
	}
	if hash, uname := extractJoinTarget("https://t.me/GotdBotGroup/12"); hash != "" || uname != "GotdBotGroup" {
		t.Fatalf("expected username GotdBotGroup, got hash=%q uname=%q", hash, uname)
	}
}

func TestExtendedPluginsEnabledAndPMPermitDisabled(t *testing.T) {
	s := &Service{}
	enabledMods := []PluginModule{
		PluginAdmin, PluginBroadcast, PluginExtra, PluginGlobals,
		PluginInfo, PluginProfile, PluginSticker, PluginSudo, PluginVCTools,
	}
	for _, mod := range enabledMods {
		if !s.IsPluginEnabled(mod) {
			t.Fatalf("expected module %v to be enabled", mod)
		}
	}
	if s.IsPluginEnabled(PluginPMPermit) {
		t.Fatalf("expected PluginPMPermit to remain disabled")
	}

	expected := map[string]PluginModule{
		"ban":         PluginAdmin,
		"zombies":     PluginAdmin,
		"fgcast":      PluginBroadcast,
		"spamcast":    PluginBroadcast,
		"ai":          PluginExtra,
		"azan":        PluginExtra,
		"font":        PluginExtra,
		"quotly":      PluginExtra,
		"gban":        PluginGlobals,
		"lsgmute":     PluginGlobals,
		"id":          PluginInfo,
		"sg":          PluginInfo,
		"setbio":      PluginProfile,
		"setusername": PluginProfile,
		"kang":        PluginSticker,
		"strinfo":     PluginSticker,
		"lsudo":       PluginSudo,
		"clearsudo":   PluginSudo,
		"startvc":     PluginVCTools,
		"ytmusic":     PluginVCTools,
	}
	for cmd, want := range expected {
		if got, ok := pluginForCommand(cmd); !ok || got != want {
			t.Fatalf("pluginForCommand(%q) = (%v, %v), want (%v, true)", cmd, got, ok, want)
		}
	}
}
