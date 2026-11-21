package databases

import (
	"strconv"
	"strings"
)

func NormalizePeerRawID(id int64) int64 {
	if id < 0 {
		s := strconv.FormatInt(id, 10)
		if strings.HasPrefix(s, "-100") {
			if v, err := strconv.ParseInt(strings.TrimPrefix(s, "-100"), 10, 64); err == nil {
				return v
			}
		}
		return -id
	}
	return id
}

func ChannelBotID(rawID int64) int64 {
	if rawID == 0 {
		return 0
	}
	if rawID < 0 {
		s := strconv.FormatInt(rawID, 10)
		if strings.HasPrefix(s, "-100") {
			return rawID
		}
		rawID = -rawID
	}
	if parsed, err := strconv.ParseInt("-100"+strconv.FormatInt(rawID, 10), 10, 64); err == nil {
		return parsed
	}
	return -rawID
}

func SameGroupID(a, b int64) bool {
	if a == 0 || b == 0 {
		return false
	}
	return a == b || NormalizePeerRawID(a) == NormalizePeerRawID(b)
}

func CanonicalGroupChatID(peerType, target string, chatID int64) int64 {
	if chatID != 0 {
		if chatID < 0 {
			if peerType == "channel" && !strings.HasPrefix(strconv.FormatInt(chatID, 10), "-100") {
				return ChannelBotID(chatID)
			}
			return chatID
		}
		if peerType == "channel" {
			return ChannelBotID(chatID)
		}
		cleanTarget := strings.TrimSpace(target)
		if strings.HasPrefix(cleanTarget, "-100") {
			return ChannelBotID(chatID)
		}
		if strings.HasPrefix(cleanTarget, "-") {
			return -chatID
		}
		return chatID
	}
	cleanTarget := strings.TrimSpace(target)
	if strings.HasPrefix(cleanTarget, "-100") {
		if id, err := strconv.ParseInt(cleanTarget, 10, 64); err == nil && id < 0 {
			return id
		}
	}
	if strings.HasPrefix(cleanTarget, "-") {
		if id, err := strconv.ParseInt(cleanTarget, 10, 64); err == nil && id < 0 {
			return id
		}
	}
	return 0
}

func FormatGroupDisplayTarget(g GroupEntry) string {
	t := strings.TrimSpace(g.Target)
	if strings.HasPrefix(t, "@") {
		return t
	}
	if g.ChatID != 0 {
		if canon := CanonicalGroupChatID(g.PeerType, t, g.ChatID); canon != 0 {
			if strings.HasPrefix(t, "-100") || g.PeerType == "channel" || canon < 0 {
				return strconv.FormatInt(canon, 10)
			}
		}
		if strings.HasPrefix(t, "-") {
			return t
		}
		if g.PeerType == "channel" {
			return strconv.FormatInt(ChannelBotID(g.ChatID), 10)
		}
		if g.PeerType == "chat" && t != strconv.FormatInt(g.ChatID, 10) {
			return t
		}
	}
	if t != "" {
		return t
	}
	if g.ChatID != 0 {
		return strconv.FormatInt(g.ChatID, 10)
	}
	return "-"
}

func FormatGroupDisplayChatID(g GroupEntry) int64 {
	if g.ChatID == 0 {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(g.Target), 10, 64); err == nil {
			return CanonicalGroupChatID(g.PeerType, g.Target, parsed)
		}
		return 0
	}
	return CanonicalGroupChatID(g.PeerType, g.Target, g.ChatID)
}

func (g GroupEntry) DisplayTarget() string {
	return FormatGroupDisplayTarget(g)
}

func (g GroupEntry) DisplayChatID() int64 {
	return FormatGroupDisplayChatID(g)
}
