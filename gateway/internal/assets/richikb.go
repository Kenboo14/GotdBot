package assets

import "fmt"

// ─── Rich HTML Templates ──────────────────────────────────────────────────────
// richikb.go contains helpers for rich structured messages:
//   - HTML tables (status dashboard, install success, list summary)
//   - Expandable <blockquote> blocks
//   - h1 / hr section headers
//
// All functions return plain HTML strings (parse mode = "HTML").

// ─── Status Dashboard ─────────────────────────────────────────────────────────

// StatusRow is one row in a status table.
type StatusRow struct {
	Label string
	Value string // already HTML-escaped by caller if needed
}

// RichStatusTable renders a full <table> from a slice of rows.
func RichStatusTable(rows []StatusRow) string {
	var buf []byte
	buf = append(buf, `<table border="1"><tr><th>⚙️ Parameter</th><th>📌 Nilai</th></tr>`...)
	for _, r := range rows {
		buf = append(buf, fmt.Sprintf(`<tr><td>%s</td><td><code>%s</code></td></tr>`, r.Label, r.Value)...)
	}
	buf = append(buf, `</table>`...)
	return string(buf)
}

// RichStatusDashboard builds the full status page HTML.
// tableHTML = output of RichStatusTable, extraInfo = tindakan terkini + terakhir.
func RichStatusDashboard(tableHTML, tindakanTerkini, tindakanTerakhir string) string {
	return `<h1>💎 Status Userbot</h1>` + "\n" +
		tableHTML + "\n" +
		`<blockquote expandable>` +
		`📋 <b>Tindakan Terkini :</b> <i>` + tindakanTerkini + `</i><br/>` + "\n" +
		`🔄 <b>Tindakan Terakhir :</b> <i>` + tindakanTerakhir + `</i>` +
		`</blockquote>` + "\n" +
		`<hr/>` + "\n" +
		`<p>GotdBot • Userbot Status Dashboard</p>`
}

// ─── Remote Status Dashboard ──────────────────────────────────────────────────

// RichControlStatusDashboard builds the remote status page HTML.
func RichControlStatusDashboard(targetName string, targetID int64, tableHTML, tindakanTerkini, tindakanTerakhir string) string {
	return fmt.Sprintf(`<h1>💎 Status Remote: %s</h1>`+"\n"+
		`<code>ID: %d</code>`+"\n"+
		tableHTML+"\n"+
		`<blockquote expandable>`+
		`📋 <b>Tindakan Terkini :</b> <i>%s</i><br/>`+"\n"+
		`🔄 <b>Tindakan Terakhir :</b> <i>%s</i>`+
		`</blockquote>`+"\n"+
		`<hr/>`+"\n"+
		`<p>GotdBot • Remote Status Dashboard</p>`,
		targetName, targetID, tindakanTerkini, tindakanTerakhir,
	)
}

// ─── Install Success ──────────────────────────────────────────────────────────

// RichInstallSuccess renders the "Instalasi Berhasil" rich message.
func RichInstallSuccess(tier, expFormatted, sisaDur, tokenSHA string) string {
	return fmt.Sprintf(
		`<h1>✅ Instalasi Berhasil</h1>`+
			`<blockquote expandable>Userbot Anda telah berhasil dipasang dan terhubung dengan aman via MTProto.</blockquote>`+
			`<table border="1">`+
			`<tr><th>Properti</th><th>Keterangan</th></tr>`+
			`<tr><td>🎖️ Tier Lisensi</td><td><code>%s</code></td></tr>`+
			`<tr><td>📅 Aktif Hingga</td><td><code>%s</code></td></tr>`+
			`<tr><td>⏳ Sisa Masa Aktif</td><td><code>%s</code></td></tr>`+
			`<tr><td>🔑 Token SHA</td><td><code>%s</code></td></tr>`+
			`</table>`+
			`<hr/>`+
			`<p>Simpan Token SHA di atas sebagai cadangan pemulihan akun Anda.</p>`,
		tier, expFormatted, sisaDur, tokenSHA,
	)
}

// ─── List / Grup Summary ──────────────────────────────────────────────────────

// RichListSummary renders the "Ringkasan List Promosi" with item rows.
// items: slice of already-formatted strings like "1. <b>List #1</b> — <code>ID: 123</code>".
func RichListSummary(total int, items []string, truncated int) string {
	body := fmt.Sprintf(`<h1>📋 Ringkasan List Promosi</h1>`+"\n"+
		`<blockquote expandable>Total tersimpan: <b>%d materi</b>`+"\n", total)
	for _, it := range items {
		body += it + "\n"
	}
	if truncated > 0 {
		body += fmt.Sprintf(`<i>... dan <code>%d</code> list lainnya.</i>`, truncated)
	}
	body += `</blockquote>` + "\n" +
		`<p>Gunakan menu <b>📋 Cek List</b> untuk melihat preview detail interaktif.</p>`
	return body
}

// RichGrupSummary renders the "Ringkasan Grup Target" with item rows.
func RichGrupSummary(total int, items []string, truncated int) string {
	body := fmt.Sprintf(`<h1>📚 Ringkasan Grup Target</h1>`+"\n"+
		`<blockquote expandable>Total tersimpan: <b>%d grup</b>`+"\n", total)
	for _, it := range items {
		body += it + "\n"
	}
	if truncated > 0 {
		body += fmt.Sprintf(`<i>... dan <code>%d</code> grup lainnya.</i>`, truncated)
	}
	body += `</blockquote>` + "\n" +
		`<p>Gunakan menu <b>📚 Cek Grup</b> untuk melihat daftar lengkap tiap grup.</p>`
	return body
}

// ─── Cache / List Saved Notice (ubot → user) ─────────────────────────────────

// RichListBerhasil renders the "List Berhasil Disimpan" notice sent by ubot.
// tipeMateri: "Teks", "Media", dll. cacheChannel: "@AiotdCaches". topicID: 3325. cacheMsgID: 7804.
func RichListBerhasil(tipeMateri, cacheChannel string, topicID, cacheMsgID, totalList int) string {
	return fmt.Sprintf(
		`<h1>✅ List Berhasil Disimpan</h1>`+
			`<blockquote expandable>Materi promosi (%s) telah diamankan ke <a href="https://t.me/%s">@%s</a> (Topic: %d) dengan Cache Message ID: <b>#%d</b></blockquote>`+
			`<table border="1">`+
			`<tr><th>🗂️ Tipe Materi</th><th>Total List Tersimpan</th></tr>`+
			`<tr><td>%s</td><td><code>%d list</code></td></tr>`+
			`</table>`+
			`<hr/><p>GotdBot • Promotional Content Manager</p>`,
		tipeMateri, cacheChannel, cacheChannel, topicID, cacheMsgID,
		tipeMateri, totalList,
	)
}

// ─── Error / Info Snippets ────────────────────────────────────────────────────

// RichErrorBlock renders a simple error notification bubble.
func RichErrorBlock(title, detail string) string {
	return fmt.Sprintf(`<h1>❌ %s</h1><blockquote expandable>%s</blockquote>`, title, detail)
}

// RichInfoBlock renders a simple info notification bubble.
func RichInfoBlock(title, detail string) string {
	return fmt.Sprintf(`<h1>ℹ️ %s</h1><blockquote expandable>%s</blockquote>`, title, detail)
}

// RichSuccessBlock renders a simple success notification bubble.
func RichSuccessBlock(title, detail string) string {
	return fmt.Sprintf(`<h1>✅ %s</h1><blockquote expandable>%s</blockquote>`, title, detail)
}
