package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"fmt"
	"html"
	"strings"
)

func (h *Handler) formatHelpHomeDisabledSummary(ctx context.Context, userID int64) string {
	var disabled []string
	for _, cat := range helpCatalog {
		var catOff []string
		for _, cmd := range cat.Commands {
			if !(cmd.PluginOn && h.isEffectiveCommandEnabled(ctx, userID, cat.Key, cmd.Key)) {
				catOff = append(catOff, "!"+cmd.Key)
			}
		}
		if len(catOff) == len(cat.Commands) && len(catOff) > 0 {
			disabled = append(disabled, cat.Title)
		} else if len(catOff) > 0 {
			disabled = append(disabled, catOff...)
		}
	}
	if len(disabled) == 0 {
		return "Tidak ada"
	}
	return strings.Join(disabled, ", ")
}

func (h *Handler) formatHelpCategoryDisabledSummary(ctx context.Context, userID int64, cat helpCategorySpec) (int, string) {
	activeCount := 0
	var disabled []string
	for _, cmd := range cat.Commands {
		if cmd.PluginOn && h.isEffectiveCommandEnabled(ctx, userID, cat.Key, cmd.Key) {
			activeCount++
		} else {
			disabled = append(disabled, "!"+cmd.Key)
		}
	}
	if len(disabled) == 0 {
		return activeCount, "Tidak ada"
	}
	if len(disabled) == len(cat.Commands) {
		return activeCount, "Semua (" + strings.Join(disabled, ", ") + ")"
	}
	return activeCount, strings.Join(disabled, ", ")
}

func buildLiveHelpCommandPage(cat helpCategorySpec, cmd helpSubCommandSpec) helpPageSnapshot {
	pluginStatus := "Load Plugin : Off"
	disabledLabel := "!" + cmd.Key
	if cmd.PluginOn {
		pluginStatus = "Load Plugin : On"
		disabledLabel = "Tidak ada"
	}
	text := fmt.Sprintf(
		`<h1>%s %s • %s</h1>
<blockquote expandable>%s</blockquote>
<table border="1">
  <tr><th>Properti</th><th>Keterangan</th></tr>
  <tr><td>📂 Kategori</td><td><code>%s</code></td></tr>
  <tr><td>⌨️ Penggunaan</td><td><code>%s</code></td></tr>
  <tr><td>🔌 Status</td><td><code>%s</code></td></tr>
  <tr><td>🚫 Dimatikan</td><td><code>%s</code></td></tr>
</table>
<hr/>
<p>Tekan <b>Back</b> untuk kembali ke sub menu <b>%s</b>.</p>`,
		cat.Emoji,
		html.EscapeString(cat.Title),
		html.EscapeString(cmd.Label),
		html.EscapeString(cmd.Description),
		html.EscapeString(cat.Title),
		html.EscapeString(cmd.Usage),
		html.EscapeString(pluginStatus),
		html.EscapeString(disabledLabel),
		html.EscapeString(cat.Title),
	)

	return helpPageSnapshot{
		text: text,
		markup: &botgotd.InlineKeyboardMarkup{
			InlineKeyboard: [][]botgotd.InlineKeyboardButton{
				{{Text: "Back", CallbackData: "assist_help:cat:" + cat.Key}},
			},
		},
	}
}

func (h *Handler) buildLiveHelpHomePage(ctx context.Context, userID int64) helpPageSnapshot {
	buttons := make([]botgotd.InlineKeyboardButton, 0, len(helpCatalog))
	activeCats := 0
	for _, cat := range helpCatalog {
		active := 0
		for _, cmd := range cat.Commands {
			if cmd.PluginOn && h.isEffectiveCommandEnabled(ctx, userID, cat.Key, cmd.Key) {
				active++
			}
		}
		if active > 0 {
			activeCats++
		}
		buttons = append(buttons, botgotd.InlineKeyboardButton{
			Text:         cat.Title,
			CallbackData: "assist_help:cat:" + cat.Key,
		})
	}
	disabledSummary := h.formatHelpHomeDisabledSummary(ctx, userID)
	text := fmt.Sprintf(
		`<h1>🤖 Asisten GO UserBot</h1>
<blockquote expandable>Pilih kategori utama di bawah untuk melihat seluruh sub perintah beserta status modul <b>GO UserBot</b>.</blockquote>
<table border="1">
  <tr><th>Informasi</th><th>Status</th></tr>
  <tr><td>🔌 Modul Aktif</td><td><code>%d / %d Kategori</code></td></tr>
  <tr><td>🚫 Dimatikan</td><td><code>%s</code></td></tr>
</table>
<hr/>
<p>Tekan tombol kategori di bawah untuk detail perintah:</p>`,
		activeCats,
		len(helpCatalog),
		html.EscapeString(disabledSummary),
	)
	return helpPageSnapshot{
		text:   text,
		markup: buildButtonGridWithBack(buttons, ""),
	}
}

func (h *Handler) buildLiveHelpCategoryPage(ctx context.Context, userID int64, cat helpCategorySpec) helpPageSnapshot {
	subButtons := make([]botgotd.InlineKeyboardButton, 0, len(cat.Commands))
	for _, cmd := range cat.Commands {
		subButtons = append(subButtons, botgotd.InlineKeyboardButton{
			Text:         cmd.Label,
			CallbackData: fmt.Sprintf("assist_help:cmd:%s:%s", cat.Key, cmd.Key),
		})
	}
	activeCount, disabledSummary := h.formatHelpCategoryDisabledSummary(ctx, userID, cat)

	pluginSummary := "Load Plugin : Off"
	if activeCount == len(cat.Commands) && activeCount > 0 {
		pluginSummary = "Load Plugin : On"
	} else if activeCount > 0 {
		pluginSummary = fmt.Sprintf("Load Plugin : %d/%d On", activeCount, len(cat.Commands))
	}

	text := fmt.Sprintf(
		`<h1>%s Menu %s</h1>
<blockquote expandable>%s</blockquote>
<table border="1">
  <tr><th>Informasi</th><th>Nilai</th></tr>
  <tr><td>📦 Modul</td><td><code>%s</code></td></tr>
  <tr><td>🔌 Status</td><td><code>%s</code></td></tr>
  <tr><td>📑 Sub Command</td><td><code>%d perintah</code></td></tr>
  <tr><td>🚫 Dimatikan</td><td><code>%s</code></td></tr>
</table>
<hr/>
<p>Pilih salah satu tombol di bawah untuk melihat detail perintah:</p>`,
		cat.Emoji,
		html.EscapeString(cat.Title),
		html.EscapeString(cat.Description),
		html.EscapeString(cat.Title),
		html.EscapeString(pluginSummary),
		len(cat.Commands),
		html.EscapeString(disabledSummary),
	)

	return helpPageSnapshot{
		text:   text,
		markup: buildButtonGridWithBack(subButtons, "assist_help:home"),
	}
}

func (h *Handler) resolveHelpPage(ctx context.Context, userID int64, data string) (helpPageSnapshot, bool) {
	resolvedKey := data
	switch data {
	case "assist_help:notes":
		resolvedKey = "assist_help:cat:notes"
	case "assist_help:ping":
		resolvedKey = "assist_help:cmd:info:ping"
	case "assist_help:code":
		resolvedKey = "assist_help:cmd:extra:code"
	case "assist_help:server":
		resolvedKey = "assist_help:cmd:info:server"
	}

	if resolvedKey == "assist_help:home" {
		return h.buildLiveHelpHomePage(ctx, userID), true
	}

	if strings.HasPrefix(resolvedKey, "assist_help:cat:") {
		catKey := strings.TrimPrefix(resolvedKey, "assist_help:cat:")
		for _, cat := range helpCatalog {
			if cat.Key == catKey {
				return h.buildLiveHelpCategoryPage(ctx, userID, cat), true
			}
		}
	}

	if strings.HasPrefix(resolvedKey, "assist_help:cmd:") {
		rest := strings.TrimPrefix(resolvedKey, "assist_help:cmd:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) == 2 {
			catKey, cmdKey := parts[0], parts[1]
			for _, cat := range helpCatalog {
				if cat.Key != catKey {
					continue
				}
				for _, cmd := range cat.Commands {
					if cmd.Key == cmdKey {
						cmd.PluginOn = cmd.PluginOn && h.isEffectiveCommandEnabled(ctx, userID, cat.Key, cmd.Key)
						return buildLiveHelpCommandPage(cat, cmd), true
					}
				}
			}
		}
	}

	snap, ok := cachedHelpPages[resolvedKey]
	return snap, ok
}
