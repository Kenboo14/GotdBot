package controllers

import (
	"assterisk/gateway/internal/databases"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	maxInlineBackupCodeLen     = 4000
	maxSingleCardBackupCodeLen = 3400
)

type TopicBackupEntry struct {
	ChatID     int64  `json:"chat_id"`
	GroupTitle string `json:"group_title,omitempty"`
	TopicID    int    `json:"topic_id"`
	Title      string `json:"title"`
}

type TopicListBackupEntry struct {
	Name         string                `json:"name"`
	Posts        []int                 `json:"posts"`
	PlayMode     string                `json:"play_mode,omitempty"`
	LinkedTopics []databases.TopicLink `json:"linked_topics,omitempty"`
}

type PortableGroupMeta struct {
	Target     string `json:"target,omitempty"`
	PeerType   string `json:"peer_type,omitempty"`
	IsForum    bool   `json:"is_forum,omitempty"`
	AccessHash int64  `json:"access_hash,omitempty"`
}

type PortableBackupPayload struct {
	Status       bool                            `json:"status"`
	Notif        bool                            `json:"notif"`
	TypeNotif    any                             `json:"typenotif,omitempty"`
	Jeda         int                             `json:"jeda"`
	JedaGrup     int                             `json:"jedagrup"`
	JedaPutar    int                             `json:"jedaputar"`
	DelayMode    string                          `json:"delay_mode,omitempty"`
	GC           []int64                         `json:"gc"`
	GCUser       map[string]string               `json:"gcuser"`
	GCExtra      map[string]PortableGroupMeta    `json:"gcextra,omitempty"`
	NamedTargets []PortableGroupMeta             `json:"named_targets,omitempty"`
	Msg          []int                           `json:"msg"`
	Mode         any                             `json:"mode"`
	WM           string                          `json:"wm"`
	WMCacheID    int                             `json:"wm_cache_id,omitempty"`
	Timer        any                             `json:"timer"`
	Admin        any                             `json:"admin"`
	Topic        map[string][]TopicBackupEntry   `json:"topic"`
	TList        map[string]TopicListBackupEntry `json:"tlist"`
	SafeMode     bool                            `json:"safemode"`
	FloodSkip    int                             `json:"floodskip"`
	FloodSkipOn  bool                            `json:"floodskip_on,omitempty"`
	Exp          string                          `json:"exp,omitempty"`
	Type         string                          `json:"type,omitempty"`
	Email        string                          `json:"email,omitempty"`
	Extra        map[string]any                  `json:"extra,omitempty"`
}

func BuildPortableBackupPayload(cfg *databases.JasebConfig, lic *databases.UserLicense) PortableBackupPayload {
	if cfg == nil {
		cfg = &databases.JasebConfig{}
	}
	gc := make([]int64, 0, len(cfg.Groups))
	gcUser := make(map[string]string, len(cfg.Groups))
	var gcExtra map[string]PortableGroupMeta
	var namedTargets []PortableGroupMeta

	for _, g := range cfg.Groups {
		cid := g.DisplayChatID()
		if cid == 0 {
			if strings.TrimSpace(g.Target) != "" {
				namedTargets = append(namedTargets, PortableGroupMeta{
					Target:     g.Target,
					PeerType:   g.PeerType,
					IsForum:    g.IsForum,
					AccessHash: g.AccessHash,
				})
				gcUser[g.Target] = g.Title
			}
			continue
		}
		gc = append(gc, cid)
		key := strconv.FormatInt(cid, 10)
		title := strings.TrimSpace(g.Title)
		if title == "" {
			title = g.DisplayTarget()
		}
		gcUser[key] = title
		if strings.HasPrefix(strings.TrimSpace(g.Target), "@") || g.IsForum || g.AccessHash != 0 || g.PeerType != "" {
			if gcExtra == nil {
				gcExtra = make(map[string]PortableGroupMeta)
			}
			gcExtra[key] = PortableGroupMeta{
				Target:     g.Target,
				PeerType:   g.PeerType,
				IsForum:    g.IsForum,
				AccessHash: g.AccessHash,
			}
		}
	}

	topicsMap := make(map[string][]TopicBackupEntry)
	for _, tp := range cfg.Topics {
		canon := databases.CanonicalGroupChatID("channel", "", tp.ChatID)
		if canon == 0 {
			canon = tp.ChatID
		}
		key := strconv.FormatInt(canon, 10)
		topicsMap[key] = append(topicsMap[key], TopicBackupEntry{
			ChatID:     canon,
			GroupTitle: tp.GroupTitle,
			TopicID:    tp.TopicID,
			Title:      tp.Title,
		})
	}

	tlistMap := make(map[string]TopicListBackupEntry, len(cfg.TopicLists))
	for _, tl := range cfg.TopicLists {
		tlistMap[tl.Name] = TopicListBackupEntry{
			Name:         tl.Name,
			Posts:        append([]int(nil), tl.Posts...),
			PlayMode:     tl.PlayMode,
			LinkedTopics: append([]databases.TopicLink(nil), tl.LinkedTopics...),
		}
	}

	modeVal := strings.TrimSpace(cfg.Mode)
	if modeVal == "" {
		modeVal = "Normal"
	}
	timerVal := strings.TrimSpace(cfg.Timer)
	if timerVal == "" {
		timerVal = "Belum diatur"
	}
	adminVal := strings.TrimSpace(cfg.Admin)
	if adminVal == "" {
		adminVal = "Belum diatur"
	}
	expVal := strings.TrimSpace(cfg.MasaAktif)
	if lic != nil && !lic.ExpiresAt.IsZero() {
		expVal = lic.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
	}

	notifOn := !strings.Contains(strings.ToLower(cfg.Notifikasi), "nonaktif")
	floodSkipVal := cfg.SkipFloodWaitSec
	if floodSkipVal == 0 && cfg.SkipFloodWait {
		floodSkipVal = 1
	}

	var extraMap map[string]any
	if cfg.Extra != 0 {
		extraMap = map[string]any{"extra": cfg.Extra}
	}

	return PortableBackupPayload{
		Status:       strings.EqualFold(strings.TrimSpace(cfg.Status), "Berjalan"),
		Notif:        notifOn,
		TypeNotif:    cfg.Notifikasi,
		Jeda:         cfg.Delay,
		JedaGrup:     cfg.Delay,
		JedaPutar:    cfg.DelayAllGroups,
		DelayMode:    cfg.DelayMode,
		GC:           gc,
		GCUser:       gcUser,
		GCExtra:      gcExtra,
		NamedTargets: namedTargets,
		Msg:          append([]int(nil), cfg.Lists...),
		Mode:         modeVal,
		WM:           cfg.Watermark,
		WMCacheID:    cfg.WatermarkCacheID,
		Timer:        timerVal,
		Admin:        adminVal,
		Topic:        topicsMap,
		TList:        tlistMap,
		SafeMode:     cfg.Safemode,
		FloodSkip:    floodSkipVal,
		FloodSkipOn:  cfg.SkipFloodWait,
		Exp:          expVal,
		Type:         "broadcast",
		Email:        cfg.Email,
		Extra:        extraMap,
	}
}

func EncodePortableBackup(cfg *databases.JasebConfig, lic *databases.UserLicense) (string, error) {
	payload := BuildPortableBackupPayload(cfg, lic)
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal portable backup: %w", err)
	}
	return tokenPrefixBackup + base64.StdEncoding.EncodeToString(raw), nil
}

func DecodePortableBackup(raw string) (*PortableBackupPayload, error) {
	clean := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	if !strings.HasPrefix(clean, tokenPrefixBackup) {
		return nil, fmt.Errorf("missing GOTD:B prefix")
	}
	encoded := strings.TrimSpace(strings.TrimPrefix(clean, tokenPrefixBackup))
	if encoded == "" {
		return nil, fmt.Errorf("empty backup payload")
	}
	jsonBytes, err := decodeBase64Any(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode backup base64: %w", err)
	}
	var payload PortableBackupPayload
	if err := json.Unmarshal(jsonBytes, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal portable backup json: %w", err)
	}
	return &payload, nil
}

func parseBackupStringField(v any) string {
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case float64:
		return strconv.FormatInt(int64(val), 10)
	case bool:
		if val {
			return "Forward"
		}
		return "Normal"
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			if s := parseBackupStringField(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}
