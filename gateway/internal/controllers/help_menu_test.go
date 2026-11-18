package controllers

import (
	"strings"
	"testing"
)

func TestHelpInitialPrompt_NoRichTagsAndHasLoadButton(t *testing.T) {
	for _, tag := range []string{"<h1>", "<blockquote", "<table", "<hr"} {
		if strings.Contains(helpInitialPromptText, tag) {
			t.Fatalf("initial help prompt must not contain rich tag %q: %s", tag, helpInitialPromptText)
		}
	}
	if !strings.Contains(helpInitialPromptText, "<b>Berikut adalah fitur asisten</b> <i>Go Userbot</i>") {
		t.Fatalf("unexpected initial help text: %q", helpInitialPromptText)
	}
	kb := helpInitialKeyboard()
	if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 1 {
		t.Fatalf("expected 1x1 initial keyboard, got %+v", kb.InlineKeyboard)
	}
	btn := kb.InlineKeyboard[0][0]
	if btn.Text != "Load" || btn.CallbackData != "assist_help:home" {
		t.Fatalf("unexpected Load button: %+v", btn)
	}
}

func TestHelpCatalog_MainSubAndBackFlow(t *testing.T) {
	expectedMain := []string{
		"Admin", "Notes", "Broadcast", "Blacklist",
		"Extra", "Globals", "Info", "PM Permit",
		"Profile", "Sticker", "Sudo", "Vctools",
	}

	homePage, ok := cachedHelpPages["assist_help:home"]
	if !ok {
		t.Fatalf("missing assist_help:home in cachedHelpPages")
	}
	if !strings.Contains(homePage.text, "<h1>") {
		t.Fatalf("expected rich message after Load click")
	}

	var mainTexts []string
	for _, row := range homePage.markup.InlineKeyboard {
		for _, btn := range row {
			mainTexts = append(mainTexts, btn.Text)
			if len(btn.CallbackData) > 64 {
				t.Fatalf("callback_data exceeds 64 bytes: %q", btn.CallbackData)
			}
		}
	}
	if len(mainTexts) != len(expectedMain) {
		t.Fatalf("main button count = %d, want %d (%v)", len(mainTexts), len(expectedMain), mainTexts)
	}
	for i, want := range expectedMain {
		if mainTexts[i] != want {
			t.Fatalf("main button[%d] = %q, want %q", i, mainTexts[i], want)
		}
	}

	for _, cat := range helpCatalog {
		catKey := "assist_help:cat:" + cat.Key
		catPage, exists := cachedHelpPages[catKey]
		if !exists {
			t.Fatalf("missing category page %q", catKey)
		}
		lastRow := catPage.markup.InlineKeyboard[len(catPage.markup.InlineKeyboard)-1]
		if len(lastRow) != 1 || lastRow[0].Text != "Back" || lastRow[0].CallbackData != "assist_help:home" {
			t.Fatalf("category %q missing Back->home button: %+v", cat.Key, lastRow)
		}

		for _, cmd := range cat.Commands {
			cmdKey := "assist_help:cmd:" + cat.Key + ":" + cmd.Key
			if len(cmdKey) > 64 {
				t.Fatalf("callback_data exceeds 64 bytes: %q (%d)", cmdKey, len(cmdKey))
			}
			cmdPage, cmdOK := cachedHelpPages[cmdKey]
			if !cmdOK {
				t.Fatalf("missing command detail page %q", cmdKey)
			}
			wantPlugin := "Load Plugin : Off"
			if cmd.PluginOn {
				wantPlugin = "Load Plugin : On"
			}
			if !strings.Contains(cmdPage.text, wantPlugin) {
				t.Fatalf("command %q expected %q in detail:\n%s", cmdKey, wantPlugin, cmdPage.text)
			}
			if len(cmdPage.markup.InlineKeyboard) != 1 || cmdPage.markup.InlineKeyboard[0][0].Text != "Back" || cmdPage.markup.InlineKeyboard[0][0].CallbackData != catKey {
				t.Fatalf("command %q missing Back->%s button: %+v", cmdKey, catKey, cmdPage.markup.InlineKeyboard)
			}
		}
	}
}
