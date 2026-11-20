package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"fmt"
	"html"
	"strings"
)

func (h *Handler) buildLiveHelpHomePage(ctx context.Context, userID int64) helpPageSnapshot {
	states := h.getEffectivePluginStates(ctx, userID)
	buttons := make([]botgotd.InlineKeyboardButton, 0, len(helpCatalog))
	activeCats := 0
	for _, cat := range helpCatalog {
		active := 0
		for _, cmd := range cat.Commands {
			if cmd.PluginOn && h.isEffectiveCommandEnabled(ctx, userID, cat.Key, cmd.Key) {
				active++
			}
		}
		icon := "🟢"
		if active == 0 {
			icon = "🔴"
		} else {
			activeCats++
			if active < len(cat.Commands) {
				icon = "🟡"
			}
		}
		_ = states
		buttons = append(buttons, botgotd.InlineKeyboardButton{
			Text:         icon + " " + cat.Title,
			CallbackData: "assist_help:cat:" + cat.Key,
		})
	}
	text := fmt.Sprintf(
		`<h1>🤖 Asisten GO UserBot</h1>
<blockquote expandable>Pilih kategori utama di bawah untuk melihat seluruh sub perintah beserta status modul <b>GO UserBot</b>.</blockquote>
<table border="1">
  <tr><th>Informasi</th><th>Status</th></tr>
  <tr><td>🔌 Modul Aktif</td><td><code>%d / %d Kategori</code></td></tr>
</table>
<hr/>
<p>Tekan tombol kategori di bawah untuk detail perintah:</p>`,
		activeCats,
		len(helpCatalog),
	)
	return helpPageSnapshot{
		text:   text,
		markup: buildButtonGridWithBack(buttons, ""),
	}
}

func (h *Handler) buildLiveHelpCategoryPage(ctx context.Context, userID int64, cat helpCategorySpec) helpPageSnapshot {
	subButtons := make([]botgotd.InlineKeyboardButton, 0, len(cat.Commands))
	activeCount := 0
	for _, cmd := range cat.Commands {
		enabled := cmd.PluginOn && h.isEffectiveCommandEnabled(ctx, userID, cat.Key, cmd.Key)
		icon := "🔴"
		if enabled {
			activeCount++
			icon = "🟢"
		}
		subButtons = append(subButtons, botgotd.InlineKeyboardButton{
			Text:         icon + " " + cmd.Label,
			CallbackData: fmt.Sprintf("assist_help:cmd:%s:%s", cat.Key, cmd.Key),
		})
	}

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
</table>
<hr/>
<p>Pilih salah satu tombol di bawah untuk melihat detail perintah:</p>`,
		cat.Emoji,
		html.EscapeString(cat.Title),
		html.EscapeString(cat.Description),
		html.EscapeString(cat.Title),
		html.EscapeString(pluginSummary),
		len(cat.Commands),
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
						return buildHelpCommandDetailPage(cat, cmd), true
					}
				}
			}
		}
	}

	snap, ok := cachedHelpPages[resolvedKey]
	return snap, ok
}
