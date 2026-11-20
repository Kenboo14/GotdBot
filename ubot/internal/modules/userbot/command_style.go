package userbot

import (
	"html"
	"regexp"
	"strings"
	"unicode"

	"github.com/gotd/td/tg"
)

var (
	reLegacyHTMLTag   = regexp.MustCompile(`(?i)</?(?:h[1-6]|b|strong|i|em|u|ins|s|strike|del|code|pre|p|br|hr|blockquote|table|thead|tbody|tr|th|td|a|span|div|ul|ol|li|tg-spoiler|tg-emoji)(?:\s+[^>]*)?\s*/?>`)
	reHTMLBlockBreak  = regexp.MustCompile(`(?i)</(?:h[1-6]|p|tr|blockquote|li|div)>|<br\s*/?>|<hr\s*/?>`)
	reHTMLCellSep     = regexp.MustCompile(`(?i)</(?:th|td)>\s*<(?:th|td)(?:\s+[^>]*)?>`)
	reMultiBlankLines = regexp.MustCompile(`\n{3,}`)
)

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func cleanUserbotHTML(raw string) string {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.Contains(s, "<") && !strings.Contains(s, "&") {
		return strings.TrimSpace(s)
	}
	hasHTML := reLegacyHTMLTag.MatchString(s)
	if !hasHTML {
		return strings.TrimSpace(s)
	}
	s = reHTMLCellSep.ReplaceAllString(s, ": ")
	s = reHTMLBlockBreak.ReplaceAllString(s, "\n")
	s = reLegacyHTMLTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, strings.TrimSpace(line))
	}
	joined := strings.Join(out, "\n")
	joined = reMultiBlankLines.ReplaceAllString(joined, "\n\n")
	return strings.TrimSpace(joined)
}

func formatCodeBlockResponse(trimmed string) (string, []tg.MessageEntityClass, bool) {
	if !strings.HasPrefix(trimmed, "```") || len(trimmed) < 6 {
		return "", nil, false
	}
	rest := trimmed[3:]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return "", nil, false
	}
	lang := strings.TrimSpace(rest[:nl])
	afterFirst := rest[nl+1:]
	if !strings.HasSuffix(afterFirst, "```") {
		return "", nil, false
	}
	body := strings.TrimSuffix(afterFirst, "```")
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return "", nil, false
	}
	entities := []tg.MessageEntityClass{
		&tg.MessageEntityPre{
			Offset:   0,
			Length:   utf16Len(body),
			Language: lang,
		},
	}
	return body, entities, true
}

func findLabelColonIndex(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] != ':' {
			continue
		}
		if strings.HasPrefix(line[i:], "://") {
			continue
		}
		if i > 0 && i+1 < len(line) && unicode.IsDigit(rune(line[i-1])) && unicode.IsDigit(rune(line[i+1])) {
			continue
		}
		if i == len(line)-1 || line[i+1] == ' ' {
			return i
		}
	}
	return -1
}

func splitParentheticalDetail(line string) (int, int, bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 4 {
		return 0, 0, false
	}
	end := len(trimmed) - 1
	if trimmed[end] == '.' && end > 0 {
		end--
	}
	if trimmed[end] != ')' {
		return 0, 0, false
	}
	openIdx := strings.LastIndexByte(trimmed[:end], '(')
	if openIdx <= 0 {
		return 0, 0, false
	}
	mainPart := strings.TrimSpace(trimmed[:openIdx])
	if mainPart == "" {
		return 0, 0, false
	}
	return openIdx, len(trimmed), true
}

func formatUserbotResponse(raw string) (string, []tg.MessageEntityClass) {
	trimmedRaw := strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if trimmedRaw == "" {
		return "", nil
	}
	if body, entities, ok := formatCodeBlockResponse(trimmedRaw); ok {
		return body, entities
	}
	clean := cleanUserbotHTML(trimmedRaw)
	if clean == "" {
		return "", nil
	}

	rawLines := strings.Split(clean, "\n")
	normLines := make([]string, len(rawLines))
	nonEmptyCount := 0
	for i, l := range rawLines {
		normLines[i] = strings.TrimRight(l, " \t")
		if strings.TrimSpace(normLines[i]) != "" {
			nonEmptyCount++
		}
	}

	finalText := strings.Join(normLines, "\n")
	entities := make([]tg.MessageEntityClass, 0, nonEmptyCount*2)
	offset := 0
	seenNonEmpty := 0

	for i, line := range normLines {
		trimmed := strings.TrimSpace(line)
		lineLen := utf16Len(line)
		if trimmed != "" {
			leadSpaces := len(line) - len(strings.TrimLeft(line, " \t"))
			contentOffset := offset + utf16Len(line[:leadSpaces])
			contentLen := utf16Len(trimmed)

			if colonIdx := findLabelColonIndex(trimmed); colonIdx >= 0 {
				if colonIdx == len(trimmed)-1 {
					entities = append(entities, &tg.MessageEntityBold{
						Offset: contentOffset,
						Length: contentLen,
					})
				} else {
					labelPart := trimmed[:colonIdx+1]
					afterColon := trimmed[colonIdx+1:]
					valTrimmed := strings.TrimSpace(afterColon)
					entities = append(entities, &tg.MessageEntityBold{
						Offset: contentOffset,
						Length: utf16Len(labelPart),
					})
					if valTrimmed != "" {
						wsBetween := len(afterColon) - len(strings.TrimLeft(afterColon, " \t"))
						valOffset := contentOffset + utf16Len(labelPart) + utf16Len(afterColon[:wsBetween])
						entities = append(entities, &tg.MessageEntityItalic{
							Offset: valOffset,
							Length: utf16Len(valTrimmed),
						})
					}
				}
			} else if nonEmptyCount > 1 {
				if seenNonEmpty == 0 {
					entities = append(entities, &tg.MessageEntityBold{
						Offset: contentOffset,
						Length: contentLen,
					})
				} else {
					entities = append(entities, &tg.MessageEntityItalic{
						Offset: contentOffset,
						Length: contentLen,
					})
				}
			} else if openIdx, _, ok := splitParentheticalDetail(trimmed); ok {
				mainPart := strings.TrimRight(trimmed[:openIdx], " \t")
				detailPart := trimmed[openIdx:]
				if mainLen := utf16Len(mainPart); mainLen > 0 {
					entities = append(entities, &tg.MessageEntityBold{
						Offset: contentOffset,
						Length: mainLen,
					})
				}
				if detailLen := utf16Len(detailPart); detailLen > 0 {
					detailOffset := contentOffset + utf16Len(trimmed[:openIdx])
					entities = append(entities, &tg.MessageEntityItalic{
						Offset: detailOffset,
						Length: detailLen,
					})
				}
			} else {
				entities = append(entities,
					&tg.MessageEntityBold{
						Offset: contentOffset,
						Length: contentLen,
					},
					&tg.MessageEntityItalic{
						Offset: contentOffset,
						Length: contentLen,
					},
				)
			}
			seenNonEmpty++
		}
		offset += lineLen
		if i < len(normLines)-1 {
			offset++
		}
	}
	return finalText, entities
}
