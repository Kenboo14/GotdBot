package userbot

import "testing"

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
