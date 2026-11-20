package userbot

import (
	"assterisk/ubot/internal/databases"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotd/td/tg"
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

func TestParseUserCommandAllRegisteredCommandsAndNoPrefixFallback(t *testing.T) {
	if len(commandPluginMap) == 0 {
		t.Fatal("commandPluginMap should not be empty")
	}
	for cmd := range commandPluginMap {
		if _, exists := userbotCommandNames[cmd]; !exists {
			t.Fatalf("command %q in commandPluginMap is missing from userbotCommandNames", cmd)
		}
		gotName, gotArgs, ok := parseUserCommand("!"+cmd, "!")
		if !ok || gotName != cmd || gotArgs != "" {
			t.Fatalf("parseUserCommand(%q, %q) = (%q, %q, %v), want (%q, \"\", true)", "!"+cmd, "!", gotName, gotArgs, ok, cmd)
		}
		gotName, gotArgs, ok = parseUserCommand("!"+cmd+" arg1\narg2", "!")
		if !ok || gotName != cmd || gotArgs != "arg1\narg2" {
			t.Fatalf("parseUserCommand multiline(%q, %q) = (%q, %q, %v)", "!"+cmd, "!", gotName, gotArgs, ok)
		}
		gotName, gotArgs, ok = parseUserCommand(cmd+" halo", "")
		if !ok || gotName != cmd || gotArgs != "halo" {
			t.Fatalf("parseUserCommand no-prefix(%q) = (%q, %q, %v)", cmd, gotName, gotArgs, ok)
		}
		gotName, gotArgs, ok = parseUserCommand("!"+cmd+" halo", "")
		if !ok || gotName != cmd || gotArgs != "halo" {
			t.Fatalf("parseUserCommand no-prefix bang-fallback(%q) = (%q, %q, %v)", "!"+cmd, gotName, gotArgs, ok)
		}
	}

	if gotName, gotArgs, ok := parseUserCommand("!gcast\nHalo semua", "!"); !ok || gotName != "gcast" || gotArgs != "Halo semua" {
		t.Fatalf("expected multiline gcast parse, got (%q, %q, %v)", gotName, gotArgs, ok)
	}
	if gotName, gotArgs, ok := parseUserCommand("!gcast\nHalo semua", ""); !ok || gotName != "gcast" || gotArgs != "Halo semua" {
		t.Fatalf("expected multiline no-prefix fallback gcast parse, got (%q, %q, %v)", gotName, gotArgs, ok)
	}
	if gotName, _, ok := parseUserCommand(".ping", "."); !ok || gotName != "ping" {
		t.Fatalf("expected custom prefix .ping to match, got (%q, %v)", gotName, ok)
	}
	if _, _, ok := parseUserCommand("!ping", "."); ok {
		t.Fatalf("expected !ping to be rejected when custom prefix is '.'")
	}
	for _, bad := range []string{"", "!", "!tidakada", "tidakada", "!!ping"} {
		if _, _, ok := parseUserCommand(bad, ""); ok {
			t.Fatalf("expected %q to fail command parsing", bad)
		}
	}
}

func TestFormatUserbotResponseBoldItalicAndHTMLCleanup(t *testing.T) {
	text, ents := formatUserbotResponse("Informasi Pengguna\nID: 1935806583\nNama: Agetaal 🚀")
	if text != "Informasi Pengguna\nID: 1935806583\nNama: Agetaal 🚀" {
		t.Fatalf("unexpected cleaned text: %q", text)
	}
	var boldCount, italicCount int
	for _, ent := range ents {
		switch e := ent.(type) {
		case *tg.MessageEntityBold:
			boldCount++
			if e.Offset < 0 || e.Length <= 0 || e.Offset+e.Length > utf16Len(text) {
				t.Fatalf("invalid bold entity bounds: %+v", e)
			}
		case *tg.MessageEntityItalic:
			italicCount++
			if e.Offset < 0 || e.Length <= 0 || e.Offset+e.Length > utf16Len(text) {
				t.Fatalf("invalid italic entity bounds: %+v", e)
			}
		}
	}
	if boldCount != 3 || italicCount != 2 {
		t.Fatalf("expected 3 bold and 2 italic entities, got bold=%d italic=%d", boldCount, italicCount)
	}

	single, sEnts := formatUserbotResponse("Pong! 12 ms")
	if single != "Pong! 12 ms" || len(sEnts) != 2 {
		t.Fatalf("expected single-line bold+italic entities, got text=%q len=%d", single, len(sEnts))
	}

	parenText, parenEnts := formatUserbotResponse("Berhasil bergabung ke Voice Chat (call_id=42).")
	if parenText != "Berhasil bergabung ke Voice Chat (call_id=42)." || len(parenEnts) != 2 {
		t.Fatalf("unexpected paren format: %q len=%d", parenText, len(parenEnts))
	}
	if _, ok := parenEnts[0].(*tg.MessageEntityBold); !ok {
		t.Fatalf("expected first entity to be bold")
	}
	if _, ok := parenEnts[1].(*tg.MessageEntityItalic); !ok {
		t.Fatalf("expected second entity to be italic")
	}

	usageText, _ := formatUserbotResponse("Gunakan: !setvc <judul baru>")
	if !strings.Contains(usageText, "<judul baru>") {
		t.Fatalf("expected placeholder <judul baru> preserved, got %q", usageText)
	}

	htmlClean, _ := formatUserbotResponse("<h1>📖 Panduan</h1><blockquote expandable>Catatan <code>!ping</code> &amp; info</blockquote>")
	if strings.Contains(htmlClean, "<blockquote") || strings.Contains(htmlClean, "<h1>") || strings.Contains(htmlClean, "&amp;") {
		t.Fatalf("expected legacy HTML stripped and unescaped, got %q", htmlClean)
	}

	codeOut, codeEnts := formatUserbotResponse("```html\n<b>Halo</b>\n```")
	if codeOut != "<b>Halo</b>" || len(codeEnts) != 1 {
		t.Fatalf("expected pre code block preserved cleanly, got %q len=%d", codeOut, len(codeEnts))
	}
	if pre, ok := codeEnts[0].(*tg.MessageEntityPre); !ok || pre.Language != "html" {
		t.Fatalf("expected MessageEntityPre html, got %#v", codeEnts[0])
	}
}

func TestVCJoinParamsAndSessionTracker(t *testing.T) {
	params, ssrc, err := buildVCJoinParams()
	if err != nil {
		t.Fatalf("buildVCJoinParams: %v", err)
	}
	if ssrc <= 0 {
		t.Fatalf("expected positive ssrc, got %d", ssrc)
	}
	var parsed struct {
		Ufrag        string `json:"ufrag"`
		Pwd          string `json:"pwd"`
		Fingerprints []struct {
			Hash        string `json:"hash"`
			Setup       string `json:"setup"`
			Fingerprint string `json:"fingerprint"`
		} `json:"fingerprints"`
		SSRC int `json:"ssrc"`
	}
	if err := json.Unmarshal([]byte(params.Data), &parsed); err != nil {
		t.Fatalf("unmarshal vc join params: %v", err)
	}
	if parsed.Ufrag == "" || parsed.Pwd == "" || len(parsed.Fingerprints) != 1 || parsed.Fingerprints[0].Hash != "sha-256" || parsed.SSRC != ssrc {
		t.Fatalf("unexpected WebRTC join params: %+v", parsed)
	}

	key := vcSessionKey(1234, &tg.InputPeerChannel{ChannelID: 55, AccessHash: 99})
	cancelled := false
	storeVCSession(key, &tg.InputGroupCall{ID: 777, AccessHash: 888}, ssrc, func() { cancelled = true })
	if got := getStoredVCCall(key); got == nil || got.ID != 777 {
		t.Fatalf("expected stored call ID 777, got %+v", got)
	}
	popped := popVCSession(key)
	if popped == nil || popped.ssrc != ssrc || !cancelled {
		t.Fatalf("expected popped session with ssrc=%d and cancelled=true, got %+v cancelled=%v", ssrc, popped, cancelled)
	}
	if getStoredVCCall(key) != nil {
		t.Fatalf("expected stored call cleared after pop")
	}
}

func TestTelegraphContentNodesAndMediaDetection(t *testing.T) {
	photoMsg := &tg.Message{
		Message: "Baris satu\nBaris dua",
		Media: &tg.MessageMediaPhoto{
			Photo: &tg.Photo{
				ID:            9001,
				AccessHash:    12345,
				FileReference: []byte{1, 2},
				Sizes: []tg.PhotoSizeClass{
					&tg.PhotoSize{Type: "m", Size: 1024},
					&tg.PhotoSize{Type: "x", Size: 4096},
				},
			},
		},
	}
	loc, sz, isMedia := extractTelegraphImageSource(photoMsg)
	if !isMedia || sz != 4096 {
		t.Fatalf("expected photo media size 4096, got isMedia=%v sz=%d", isMedia, sz)
	}
	if pLoc, ok := loc.(*tg.InputPhotoFileLocation); !ok || pLoc.ThumbSize != "x" {
		t.Fatalf("expected largest photo thumb 'x', got %#v", loc)
	}

	nodes := buildTelegraphContentNodes(photoMsg.Message, "https://i.ibb.co/example/pic.jpg")
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes (2 p + 1 figure), got %d", len(nodes))
	}
	if nodes[2]["tag"] != "figure" {
		t.Fatalf("expected figure tag at index 2, got %v", nodes[2]["tag"])
	}
}

func TestPeerChatIDFormatAndUserInfoHelpers(t *testing.T) {
	if got := formatPeerChatID(&tg.InputPeerChannel{ChannelID: 1987654321, AccessHash: 1}, nil, 999); got != -1001987654321 {
		t.Fatalf("expected -1001987654321, got %d", got)
	}
	if got := formatPeerChatID(&tg.InputPeerChat{ChatID: 54321}, nil, 999); got != -54321 {
		t.Fatalf("expected -54321, got %d", got)
	}
	if got := formatPeerChatID(&tg.InputPeerUser{UserID: 318505206}, nil, 999); got != 318505206 {
		t.Fatalf("expected 318505206, got %d", got)
	}

	u := &tg.User{
		ID:        318505206,
		FirstName: "zarven",
		Premium:   true,
		Photo:     &tg.UserProfilePhoto{PhotoID: 1, DCID: 5},
		Status:    &tg.UserStatusRecently{},
	}
	if got := formatUserDCID(u); got != "5" {
		t.Fatalf("expected DC ID 5, got %q", got)
	}
	if got := formatUserLastSeen(u); got != "Recently" {
		t.Fatalf("expected Recently, got %q", got)
	}

	rClient, mu, strMap, _, _ := startWorkerMockRedisLocked(t)
	defer rClient.Client.Close()
	s := &Service{redis: rClient}
	tok, err := s.saveInlineCardToRedis(context.Background(), ubotInlineCardPayload{
		Title: "Info",
		HTML:  "<b>DC ID:</b> <i>5</i>",
		Buttons: [][]ubotInlineCardButton{
			{{Text: "Bio", Alert: "Halo ini bio"}, {Text: "Direct link", URL: "tg://user?id=318505206"}},
		},
	})
	if err != nil || tok == "" {
		t.Fatalf("saveInlineCardToRedis failed: tok=%q err=%v", tok, err)
	}
	mu.Lock()
	rawCard := strMap["inline:card:"+tok]
	bioVal := strMap["inline:bio:"+tok]
	mu.Unlock()
	if !strings.Contains(rawCard, "Direct link") || bioVal != "Halo ini bio" {
		t.Fatalf("unexpected Redis inline card data: card=%q bio=%q", rawCard, bioVal)
	}
}
