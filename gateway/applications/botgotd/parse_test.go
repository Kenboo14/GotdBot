package botgotd

import (
	"fmt"
	"testing"
	"unicode/utf16"
)

func TestParseHTMLSanitization(t *testing.T) {
	b := &Bot{}
	cases := []struct {
		name string
		html string
	}{
		{
			name: "Status Dashboard",
			html: `<b>📚 Status Userbot (Remote)</b>
━━━━━━━━━━━━━━━━━━━━

👑 <b>Akun Target:</b> EHEM (<code>7420876266</code>)
🚀 <b>Status:</b> <code>Aktif</code>
👑 <b>Mode:</b> <code>Standard</code>
🕒 <b>Jeda:</b> <code>10 detik</code>
🌐 <b>Target Grup:</b> <code>0 grup</code>
📚 <b>Materi List:</b> <code>0 list</code>
⚠️ <b>Safemode:</b> <code>Nonaktif</code>
⏭️ <b>Skip Flood:</b> <code>Mati</code>
⏱️ <b>Timer:</b> <code>-</code>
🔥 <b>Masa Aktif:</b> <code>Permanen</code>

<blockquote>
📋 <b>Tindakan Terkini:</b> <i>-</i><br/>
📄 <b>Tindakan Terakhir:</b> <i>-</i>
</blockquote>`,
		},
		{
			name: "Cek List Items",
			html: `<b>📋 Ringkasan List Promosi (Remote)</b>
━━━━━━━━━━━━━━━━━━━━

<blockquote>Total tersimpan: <b>2 materi</b> (Hal 1/1)

1. <b>List #1</b> — <code>Cache ID: 123456</code>
2. <b>List #2</b> — <code>Cache ID: 789012</code>
</blockquote>`,
		},
		{
			name: "Cek Grup Items",
			html: `<b>📚 Ringkasan Grup Target (Remote)</b>
━━━━━━━━━━━━━━━━━━━━

<blockquote>Total tersimpan: <b>2 grup</b> (Hal 1/1)

1. Grup Keren 🔥 — <code>-1001234567890</code>
2. Support Chat 💬 — <code>@supportchat</code>
</blockquote>`,
		},
	}

	for _, tc := range cases {
		plain, entities := b.parseHTMLToEntities(tc.html)
		u16Len := len(utf16.Encode([]rune(plain)))
		fmt.Printf("=== %s: u16Len=%d, entities=%d ===\n", tc.name, u16Len, len(entities))
		for i, e := range entities {
			end := e.GetOffset() + e.GetLength()
			if end > u16Len {
				t.Fatalf("[%s] Entity %d (%T) exceeds bounds: Offset=%d, Length=%d, End=%d > Max=%d",
					tc.name, i, e, e.GetOffset(), e.GetLength(), end, u16Len)
			}
		}
	}
}
