package userbot

import (
	"assterisk/ubot/internal/databases"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gotd/td/tg"
)

func TestBlacklistCanonicalAndDisplayIDs(t *testing.T) {
	if got := canonicalBlacklistID("group", "supergroup", 1234567890); got != -1001234567890 {
		t.Fatalf("supergroup canonical ID = %d, want -1001234567890", got)
	}
	if got := canonicalBlacklistID("channel", "channel", 2234567890); got != -1002234567890 {
		t.Fatalf("channel canonical ID = %d, want -1002234567890", got)
	}
	if got := canonicalBlacklistID("group", "basic", 987654); got != -987654 {
		t.Fatalf("basic group canonical ID = %d, want -987654", got)
	}
	if got := canonicalBlacklistID("private", "private", 777123); got != 777123 {
		t.Fatalf("private canonical ID = %d, want 777123", got)
	}
	if got := displayChatID("group", "", 2345678901); got != -1002345678901 {
		t.Fatalf("legacy supergroup displayChatID = %d, want -1002345678901", got)
	}
	if got := displayChatID("group", "basic", -987654); got != -987654 {
		t.Fatalf("basic group displayChatID = %d, want -987654", got)
	}
}

func TestBlacklistLabelFormattingAndTruncation(t *testing.T) {
	gcLabel := formatBlacklistEntryLabel("group", "Grup Jual Beli Indonesia", "", -1001234567890)
	if gcLabel != "Grup Jual " || utf8.RuneCountInString(gcLabel) != 10 {
		t.Fatalf("group label = %q (runes=%d), want max 10 runes", gcLabel, utf8.RuneCountInString(gcLabel))
	}

	chLabel := formatBlacklistEntryLabel("channel", "ChannelPromoOfficial", "", -100999888777)
	if chLabel != "ChannelPro" || utf8.RuneCountInString(chLabel) != 10 {
		t.Fatalf("channel label = %q, want ChannelPro", chLabel)
	}

	pvWithUname := formatBlacklistEntryLabel("private", "Nama Panjang Sekali", "@very_long_username", 111222)
	if pvWithUname != "@very_long" || utf8.RuneCountInString(pvWithUname) != 10 {
		t.Fatalf("private with username label = %q, want @very_long", pvWithUname)
	}

	pvFallbackName := formatBlacklistEntryLabel("private", "Budi Santoso", "", 111222)
	if pvFallbackName != "Budi Santo" || utf8.RuneCountInString(pvFallbackName) != 10 {
		t.Fatalf("private fallback name = %q, want Budi Santo", pvFallbackName)
	}

	pvShortUname := formatBlacklistEntryLabel("private", "Budi", "budi99", 111222)
	if pvShortUname != "@budi99" {
		t.Fatalf("private short username = %q, want @budi99", pvShortUname)
	}
}

func TestResolveBlacklistTarget_RemoteAndSupergroup(t *testing.T) {
	s := &Service{}
	ctx := context.Background()
	entities := tg.Entities{
		Users: map[int64]*tg.User{
			555: {ID: 555, FirstName: "Rina", LastName: "Wati", Username: "rinawati_long"},
		},
		Chats: map[int64]*tg.Chat{
			888: {ID: 888, Title: "Grup Dasar"},
		},
		Channels: map[int64]*tg.Channel{
			1234567890: {ID: 1234567890, Title: "Supergroup Dagang", Megagroup: true},
			2222222222: {ID: 2222222222, Title: "Channel Info Utama", Broadcast: true},
		},
	}

	sgMsg := &tg.Message{ID: 1, PeerID: &tg.PeerChannel{ChannelID: 1234567890}}
	sgInfo, err := s.resolveBlacklistTarget(ctx, nil, nil, sgMsg, entities, "")
	if err != nil {
		t.Fatalf("in-place supergroup err: %v", err)
	}
	if sgInfo.CanonicalID != -1001234567890 || sgInfo.Kind != "group" || sgInfo.PeerKind != "supergroup" || sgInfo.Title != "Supergroup Dagang" {
		t.Fatalf("unexpected supergroup info: %+v", sgInfo)
	}

	chMsg := &tg.Message{ID: 2, PeerID: &tg.PeerChannel{ChannelID: 2222222222}}
	chInfo, err := s.resolveBlacklistTarget(ctx, nil, nil, chMsg, entities, "")
	if err != nil {
		t.Fatalf("in-place channel err: %v", err)
	}
	if chInfo.CanonicalID != -1002222222222 || chInfo.Kind != "channel" || chInfo.PeerKind != "channel" {
		t.Fatalf("unexpected channel info: %+v", chInfo)
	}

	remoteUname, err := s.resolveBlacklistTarget(ctx, nil, nil, sgMsg, entities, "@rinawati_long")
	if err != nil {
		t.Fatalf("remote @username err: %v", err)
	}
	if remoteUname.Kind != "private" || remoteUname.CanonicalID != 555 || remoteUname.Username != "@rinawati_long" {
		t.Fatalf("unexpected remote username info: %+v", remoteUname)
	}

	remoteSG, err := s.resolveBlacklistTarget(ctx, nil, nil, &tg.Message{PeerID: &tg.PeerUser{UserID: 1}}, entities, "-1001234567890")
	if err != nil {
		t.Fatalf("remote supergroup ID err: %v", err)
	}
	if remoteSG.CanonicalID != -1001234567890 || remoteSG.Kind != "group" || remoteSG.PeerKind != "supergroup" || remoteSG.Title != "Supergroup Dagang" {
		t.Fatalf("unexpected remote supergroup info: %+v", remoteSG)
	}

	remoteCSlug, err := s.resolveBlacklistTarget(ctx, nil, nil, &tg.Message{PeerID: &tg.PeerUser{UserID: 1}}, entities, "https://t.me/c/2222222222/10")
	if err != nil {
		t.Fatalf("remote t.me/c link err: %v", err)
	}
	if remoteCSlug.CanonicalID != -1002222222222 || remoteCSlug.Kind != "channel" {
		t.Fatalf("unexpected remote t.me/c info: %+v", remoteCSlug)
	}
}

func TestGroupedBlacklistOrderingAndMetadataPersistence(t *testing.T) {
	ctx := context.Background()
	sqlite, err := databases.NewSQLiteClient(filepath.Join(t.TempDir(), "bl.db"))
	if err != nil {
		t.Fatalf("NewSQLiteClient: %v", err)
	}
	defer sqlite.Close()

	const ownerID int64 = 9001
	svc := &Service{sqlite: sqlite}

	records := []databases.BlacklistRecord{
		{OwnerID: ownerID, ChatType: "group", ChatID: -100111222333, PeerKind: "supergroup", Title: "Grup Pertama", CreatedAt: time.Now()},
		{OwnerID: ownerID, ChatType: "group", ChatID: -987654, PeerKind: "basic", Title: "Grup Kedua"},
		{OwnerID: ownerID, ChatType: "private", ChatID: 7001, PeerKind: "private", Title: "Alice Doe", Username: "@alice_superlong"},
		{OwnerID: ownerID, ChatType: "channel", ChatID: -100555666777, PeerKind: "channel", Title: "Channel Promo"},
	}
	for _, r := range records {
		if err := sqlite.UpsertBlacklist(ctx, r); err != nil {
			t.Fatalf("UpsertBlacklist(%+v): %v", r, err)
		}
	}

	grouped, err := svc.loadGroupedBlacklist(ctx, nil, ownerID)
	if err != nil {
		t.Fatalf("loadGroupedBlacklist: %v", err)
	}
	if len(grouped.groups) != 2 || len(grouped.privates) != 1 || len(grouped.channels) != 1 {
		t.Fatalf("unexpected section counts: gc=%d pv=%d ch=%d", len(grouped.groups), len(grouped.privates), len(grouped.channels))
	}
	if grouped.groups[0].ChatID != -100111222333 || grouped.groups[1].ChatID != -987654 {
		t.Fatalf("unexpected group ordering: %+v", grouped.groups)
	}
	if !strings.HasPrefix(formatBlacklistEntryLabel("private", grouped.privates[0].Title, grouped.privates[0].Username, grouped.privates[0].ChatID), "@alice_sup") {
		t.Fatalf("expected @alice_sup label, got %q", formatBlacklistEntryLabel("private", grouped.privates[0].Title, grouped.privates[0].Username, grouped.privates[0].ChatID))
	}

	rendered := formatBlacklistListHTML(grouped)
	for _, want := range []string{
		"<b>List Blacklist:</b>",
		"<b>GROUP</b>",
		"1. <code>-100111222333</code> | Grup Perta",
		"2. <code>-987654</code> | Grup Kedua",
		"<b>PRIVATE</b>",
		"1. <code>7001</code> | @alice_sup",
		"<b>CHANNEL</b>",
		"1. <code>-100555666777</code> | Channel Pr",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("formatBlacklistListHTML missing %q in:\n%s", want, rendered)
		}
	}

	if err := sqlite.RemoveBlacklistAny(ctx, ownerID, "group", []int64{grouped.groups[0].ChatID, 111222333}); err != nil {
		t.Fatalf("RemoveBlacklistAny: %v", err)
	}
	after, err := svc.loadGroupedBlacklist(ctx, nil, ownerID)
	if err != nil {
		t.Fatalf("loadGroupedBlacklist after remove: %v", err)
	}
	if len(after.groups) != 1 || after.groups[0].ChatID != -987654 {
		t.Fatalf("expected only basic group remaining after removing #1, got %+v", after.groups)
	}
}

func TestManualGroupTokenParsingAndCanonicalID(t *testing.T) {
	raw := "-1001234567890\n-987654\n@namagrup\nhttps://t.me/+InviteHash123\nt.me/joinchat/LegacyHash456\n-1001234567890"
	tokens := parseManualGroupTokens(raw)
	if len(tokens) != 5 {
		t.Fatalf("expected 5 deduplicated tokens, got %d (%v)", len(tokens), tokens)
	}
	if got := canonicalGroupID("channel", 1234567890); got != -1001234567890 {
		t.Fatalf("canonicalGroupID channel = %d, want -1001234567890", got)
	}
	if got := canonicalGroupID("chat", 5112266009); got != -5112266009 {
		t.Fatalf("canonicalGroupID basic chat = %d, want -5112266009", got)
	}
	if canon, ok := extractTelegramCChannelID("https://t.me/c/1987654321/42"); !ok || canon != -1001987654321 {
		t.Fatalf("extractTelegramCChannelID = (%d, %v), want (-1001987654321, true)", canon, ok)
	}
}
