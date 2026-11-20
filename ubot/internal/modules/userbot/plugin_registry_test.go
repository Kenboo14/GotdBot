package userbot

import (
	"context"
	"testing"
)

func TestPluginRegistry_SwitchAndCommandMapping(t *testing.T) {
	s := &Service{}

	// Implemented module defaults to On; unimplemented module defaults to Off
	if !s.IsPluginEnabled(PluginNotes) {
		t.Fatalf("expected PluginNotes to default to enabled")
	}
	if s.IsPluginEnabled(PluginPMPermit) {
		t.Fatalf("expected PluginPMPermit to default to disabled")
	}

	// Switching Notes off takes effect immediately
	s.SetPluginEnabled(PluginNotes, false)
	if s.IsPluginEnabled(PluginNotes) {
		t.Fatalf("expected PluginNotes to be disabled after SetPluginEnabled(false)")
	}
	s.SetPluginEnabled(PluginNotes, true)
	if !s.IsPluginEnabled(PluginNotes) {
		t.Fatalf("expected PluginNotes to be re-enabled")
	}

	if mod, ok := pluginForCommand("alive"); !ok || mod != PluginInfo {
		t.Fatalf("pluginForCommand(alive) = (%v, %v), want (PluginInfo, true)", mod, ok)
	}
	if mod, ok := pluginForCommand("addblock"); !ok || mod != PluginLogger {
		t.Fatalf("pluginForCommand(addblock) = (%v, %v), want (PluginLogger, true)", mod, ok)
	}
	for cmd, wantMod := range map[string]PluginModule{
		"ban":     PluginAdmin,
		"gban":    PluginGlobals,
		"setbio":  PluginProfile,
		"kang":    PluginSticker,
		"startvc": PluginVCTools,
	} {
		if mod, ok := pluginForCommand(cmd); !ok || mod != wantMod {
			t.Fatalf("pluginForCommand(%s) = (%v, %v), want (%v, true)", cmd, mod, ok, wantMod)
		}
	}
}

func TestPluginRegistry_DisabledPluginsEnv(t *testing.T) {
	t.Setenv("DISABLED_PLUGINS", "broadcast, sudo")
	s := &Service{}

	if s.IsPluginEnabled(PluginBroadcast) {
		t.Fatalf("expected PluginBroadcast disabled via DISABLED_PLUGINS")
	}
	if s.IsPluginEnabled(PluginSudo) {
		t.Fatalf("expected PluginSudo disabled via DISABLED_PLUGINS")
	}
	if !s.IsPluginEnabled(PluginInfo) {
		t.Fatalf("expected PluginInfo to remain enabled")
	}
}

func TestPluginRegistry_SyncFromRedis(t *testing.T) {
	rClient, mu, _, hashMap, _ := startWorkerMockRedisLocked(t)
	defer rClient.Client.Close()

	mu.Lock()
	hashMap[redisKeyPluginState] = map[string]string{
		"vctools":  "0",
		"sticker":  "1",
		"pmpermit": "1",
	}
	mu.Unlock()

	s := &Service{redis: rClient}
	s.SyncPluginsFromRedis(context.Background())

	if s.IsPluginEnabled(PluginVCTools) {
		t.Fatalf("expected PluginVCTools disabled from Redis hash")
	}
	if !s.IsPluginEnabled(PluginSticker) {
		t.Fatalf("expected PluginSticker enabled from Redis hash")
	}
	if s.IsPluginEnabled(PluginPMPermit) {
		t.Fatalf("expected PluginPMPermit to stay disabled regardless of Redis hash")
	}
}

func TestPluginRegistry_SubCommandAndPerOwnerOverrides(t *testing.T) {
	rClient, mu, _, hashMap, _ := startWorkerMockRedisLocked(t)
	defer rClient.Client.Close()

	mu.Lock()
	hashMap[redisKeyPluginState] = map[string]string{
		"admin":   "1",
		"cmd:ban": "0",
	}
	hashMap[redisKeyUserPluginPrefix+"7001"] = map[string]string{
		"vctools": "0",
		"cmd:ban": "1",
		"cmd:gcast": "0",
	}
	mu.Unlock()

	s := &Service{redis: rClient}
	s.SyncPluginsFromRedis(context.Background())
	s.SyncUserPluginsFromRedis(context.Background(), 7001)

	if s.IsCommandEnabledForOwner(8001, "ban") {
		t.Fatalf("expected global cmd:ban disabled for user 8001")
	}
	if !s.IsCommandEnabledForOwner(8001, "kick") {
		t.Fatalf("expected admin kick enabled for user 8001")
	}
	if !s.IsCommandEnabledForOwner(7001, "ban") {
		t.Fatalf("expected per-user override cmd:ban=1 enabled for user 7001")
	}
	if s.IsCommandEnabledForOwner(7001, "joinvc") {
		t.Fatalf("expected vctools joinvc disabled for user 7001")
	}
	if s.IsCommandEnabledForOwner(7001, "broadcast") {
		t.Fatalf("expected alias broadcast mapped to cmd:gcast disabled for user 7001")
	}
}
