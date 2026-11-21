package databases

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

func cloneTokenProfile(p *TokenProfile) *TokenProfile {
	if p == nil {
		return nil
	}
	cp := *p
	cp.Lists = slices.Clone(p.Lists)
	if cp.Lists == nil {
		cp.Lists = []int{}
	}
	cp.Groups = slices.Clone(p.Groups)
	if cp.Groups == nil {
		cp.Groups = []GroupEntry{}
	}
	cp.Topics = slices.Clone(p.Topics)
	if cp.Topics == nil {
		cp.Topics = []TopicEntry{}
	}
	if p.TopicLists != nil {
		cp.TopicLists = make([]TopicListEntry, len(p.TopicLists))
		for i, entry := range p.TopicLists {
			ecp := entry
			ecp.Posts = slices.Clone(entry.Posts)
			if ecp.Posts == nil {
				ecp.Posts = []int{}
			}
			ecp.LinkedTopics = slices.Clone(entry.LinkedTopics)
			if ecp.LinkedTopics == nil {
				ecp.LinkedTopics = []TopicLink{}
			}
			cp.TopicLists[i] = ecp
		}
	} else {
		cp.TopicLists = []TopicListEntry{}
	}
	return &cp
}

func TokenProfileFromConfig(tokenSHA string, cfg *JasebConfig) *TokenProfile {
	if tokenSHA == "" || cfg == nil {
		return nil
	}
	p := &TokenProfile{
		TokenSHA:         tokenSHA,
		OwnerChatID:      cfg.OwnerID,
		Type:             cfg.Type,
		Delay:            cfg.Delay,
		DelayMode:        cfg.DelayMode,
		DelayAllGroups:   cfg.DelayAllGroups,
		SkipFloodWait:    cfg.SkipFloodWait,
		SkipFloodWaitSec: cfg.SkipFloodWaitSec,
		Timer:            cfg.Timer,
		Extra:            cfg.Extra,
		Mode:             cfg.Mode,
		Safemode:         cfg.Safemode,
		Notifikasi:       cfg.Notifikasi,
		Email:            cfg.Email,
		Admin:            cfg.Admin,
		Watermark:        cfg.Watermark,
		WatermarkCacheID: cfg.WatermarkCacheID,
		TopicEnabled:     cfg.TopicEnabled,
		Lists:            cfg.Lists,
		Groups:           cfg.Groups,
		Topics:           cfg.Topics,
		TopicLists:       cfg.TopicLists,
		UpdatedAt:        time.Now(),
	}
	if strings.TrimSpace(p.Type) == "" {
		p.Type = "Broadcast"
	}
	if strings.TrimSpace(p.DelayMode) == "" {
		p.DelayMode = "manual"
	}
	if p.Delay <= 0 && p.DelayMode == "manual" && p.DelayAllGroups <= 0 {
		p.Delay = 5
	}
	if strings.TrimSpace(p.Timer) == "" {
		p.Timer = "Belum diatur"
	}
	if strings.TrimSpace(p.Mode) == "" {
		p.Mode = "Normal"
	}
	if strings.TrimSpace(p.Notifikasi) == "" {
		p.Notifikasi = "Aktif · Kirim ke Saya"
	}
	if strings.TrimSpace(p.Email) == "" {
		p.Email = "Belum diatur"
	}
	if strings.TrimSpace(p.Admin) == "" {
		p.Admin = "Belum diatur"
	}
	return cloneTokenProfile(p)
}

func NewDefaultTokenProfile(tokenSHA string, ownerChatID int64) *TokenProfile {
	return &TokenProfile{
		TokenSHA:         tokenSHA,
		OwnerChatID:      ownerChatID,
		Type:             "Broadcast",
		Delay:            5,
		DelayMode:        "manual",
		DelayAllGroups:   0,
		SkipFloodWait:    false,
		SkipFloodWaitSec: 0,
		Timer:            "Belum diatur",
		Extra:            0,
		Mode:             "Normal",
		Safemode:         false,
		Notifikasi:       "Aktif · Kirim ke Saya",
		Email:            "Belum diatur",
		Admin:            "Belum diatur",
		Watermark:        "",
		WatermarkCacheID: 0,
		TopicEnabled:     false,
		Lists:            []int{},
		Groups:           []GroupEntry{},
		Topics:           []TopicEntry{},
		TopicLists:       []TopicListEntry{},
		UpdatedAt:        time.Now(),
	}
}

func ApplyTokenProfileToConfig(p *TokenProfile, cfg *JasebConfig) {
	if p == nil || cfg == nil {
		return
	}
	cp := cloneTokenProfile(p)
	cfg.ActiveTokenSHA = cp.TokenSHA
	cfg.Type = cp.Type
	cfg.Delay = cp.Delay
	cfg.DelayMode = cp.DelayMode
	cfg.DelayAllGroups = cp.DelayAllGroups
	cfg.SkipFloodWait = cp.SkipFloodWait
	cfg.SkipFloodWaitSec = cp.SkipFloodWaitSec
	cfg.Timer = cp.Timer
	cfg.Extra = cp.Extra
	cfg.Mode = cp.Mode
	cfg.Safemode = cp.Safemode
	cfg.Notifikasi = cp.Notifikasi
	cfg.Email = cp.Email
	cfg.Admin = cp.Admin
	cfg.Watermark = cp.Watermark
	cfg.WatermarkCacheID = cp.WatermarkCacheID
	cfg.TopicEnabled = cp.TopicEnabled
	cfg.Lists = cp.Lists
	cfg.Groups = cp.Groups
	cfg.Topics = cp.Topics
	cfg.TopicLists = cp.TopicLists
	cfg.UpdatedAt = time.Now()
}

func selectActiveLicense(licenses []*UserLicense, activeTokenSHA string) *UserLicense {
	if activeTokenSHA != "" {
		for _, lic := range licenses {
			if lic != nil && lic.Status == "active" && lic.TokenSHA == activeTokenSHA {
				return lic
			}
		}
	}
	var best *UserLicense
	for _, lic := range licenses {
		if lic == nil || lic.Status != "active" {
			continue
		}
		if IsBetterActiveLicense(lic, best) {
			best = lic
		}
	}
	return best
}

func sortActiveLicenses(licenses []*UserLicense) {
	sort.Slice(licenses, func(i, j int) bool {
		ri := TierRank(licenses[i].Tier)
		rj := TierRank(licenses[j].Tier)
		if ri != rj {
			return ri > rj
		}
		if !licenses[i].ExpiresAt.Equal(licenses[j].ExpiresAt) {
			return licenses[i].ExpiresAt.After(licenses[j].ExpiresAt)
		}
		return licenses[i].TokenSHA < licenses[j].TokenSHA
	})
}

func (s *SQLiteClient) SaveTokenProfile(ctx context.Context, p *TokenProfile) error {
	if p == nil || strings.TrimSpace(p.TokenSHA) == "" {
		return nil
	}
	cp := cloneTokenProfile(p)
	if cp.UpdatedAt.IsZero() {
		cp.UpdatedAt = time.Now()
	}
	if strings.TrimSpace(cp.DelayMode) == "" {
		cp.DelayMode = "manual"
	}
	sfw := 0
	if cp.SkipFloodWait {
		sfw = 1
	}
	sfm := 0
	if cp.Safemode {
		sfm = 1
	}
	tpe := 0
	if cp.TopicEnabled {
		tpe = 1
	}
	listsBytes, _ := json.Marshal(cp.Lists)
	groupsBytes, _ := json.Marshal(cp.Groups)
	topicsBytes, _ := json.Marshal(cp.Topics)
	topicListsBytes, _ := json.Marshal(cp.TopicLists)

	query := `INSERT INTO token_profiles (
		token_sha, owner_chat_id, type, delay, delay_mode, delay_all_groups, skip_floodwait, skip_floodwait_sec,
		timer, extra, mode, safemode, notifikasi, email, admin, watermark, watermark_cache_id, topic_enabled,
		lists, groups, topics, topic_lists, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(token_sha) DO UPDATE SET
		owner_chat_id = excluded.owner_chat_id,
		type = excluded.type,
		delay = excluded.delay,
		delay_mode = excluded.delay_mode,
		delay_all_groups = excluded.delay_all_groups,
		skip_floodwait = excluded.skip_floodwait,
		skip_floodwait_sec = excluded.skip_floodwait_sec,
		timer = excluded.timer,
		extra = excluded.extra,
		mode = excluded.mode,
		safemode = excluded.safemode,
		notifikasi = excluded.notifikasi,
		email = excluded.email,
		admin = excluded.admin,
		watermark = excluded.watermark,
		watermark_cache_id = excluded.watermark_cache_id,
		topic_enabled = excluded.topic_enabled,
		lists = excluded.lists,
		groups = excluded.groups,
		topics = excluded.topics,
		topic_lists = excluded.topic_lists,
		updated_at = excluded.updated_at;`

	_, err := s.DB.ExecContext(ctx, query,
		cp.TokenSHA, cp.OwnerChatID, cp.Type, cp.Delay, cp.DelayMode, cp.DelayAllGroups, sfw, cp.SkipFloodWaitSec,
		cp.Timer, cp.Extra, cp.Mode, sfm, cp.Notifikasi, cp.Email, cp.Admin, cp.Watermark, cp.WatermarkCacheID, tpe,
		string(listsBytes), string(groupsBytes), string(topicsBytes), string(topicListsBytes), cp.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite save token profile: %w", err)
	}
	return nil
}

func (s *SQLiteClient) GetTokenProfile(ctx context.Context, tokenSHA string) (*TokenProfile, error) {
	if strings.TrimSpace(tokenSHA) == "" {
		return nil, nil
	}
	query := `SELECT token_sha, owner_chat_id, type, delay, COALESCE(delay_mode, 'manual'), COALESCE(delay_all_groups, 0),
		skip_floodwait, COALESCE(skip_floodwait_sec, 0), timer, extra, mode, safemode, notifikasi, email, admin,
		watermark, watermark_cache_id, topic_enabled, lists, groups, topics, COALESCE(topic_lists, '[]'), updated_at
		FROM token_profiles WHERE token_sha = ? LIMIT 1;`

	var p TokenProfile
	var sfw, sfm, tpe int
	var listsJSON, groupsJSON, topicsJSON, topicListsJSON string

	err := s.DB.QueryRowContext(ctx, query, tokenSHA).Scan(
		&p.TokenSHA, &p.OwnerChatID, &p.Type, &p.Delay, &p.DelayMode, &p.DelayAllGroups,
		&sfw, &p.SkipFloodWaitSec, &p.Timer, &p.Extra, &p.Mode, &sfm, &p.Notifikasi, &p.Email, &p.Admin,
		&p.Watermark, &p.WatermarkCacheID, &tpe, &listsJSON, &groupsJSON, &topicsJSON, &topicListsJSON, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite get token profile: %w", err)
	}

	p.SkipFloodWait = sfw == 1
	p.Safemode = sfm == 1
	p.TopicEnabled = tpe == 1

	if listsJSON != "" {
		_ = json.Unmarshal([]byte(listsJSON), &p.Lists)
	}
	if groupsJSON != "" {
		_ = json.Unmarshal([]byte(groupsJSON), &p.Groups)
	}
	if topicsJSON != "" {
		_ = json.Unmarshal([]byte(topicsJSON), &p.Topics)
	}
	if topicListsJSON != "" {
		_ = json.Unmarshal([]byte(topicListsJSON), &p.TopicLists)
	}
	return cloneTokenProfile(&p), nil
}

func (s *SQLiteClient) GetActiveTokenSHA(ctx context.Context, ownerID int64) (string, error) {
	if ownerID == 0 {
		return "", nil
	}
	var sha string
	err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(active_token_sha, '') FROM jaseb_configurasi WHERE owner_id = ? LIMIT 1;`,
		ownerID,
	).Scan(&sha)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("sqlite get active token sha: %w", err)
	}
	return sha, nil
}

func (s *SQLiteClient) ListActiveUserLicenses(ctx context.Context, ownerChatID int64) ([]*UserLicense, error) {
	query := `SELECT token_sha, master_token, owner_chat_id, userbot_id, tier, can_custom_watermark, expires_at, created_at, status
		FROM user_licenses WHERE (owner_chat_id = ? OR userbot_id = ?) AND status = 'active';`
	rows, err := s.DB.QueryContext(ctx, query, ownerChatID, ownerChatID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list active user licenses: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	var result []*UserLicense
	for rows.Next() {
		var lic UserLicense
		var ccw int
		if err := rows.Scan(
			&lic.TokenSHA, &lic.MasterToken, &lic.OwnerChatID, &lic.UserbotID,
			&lic.Tier, &ccw, &lic.ExpiresAt, &lic.CreatedAt, &lic.Status,
		); err != nil {
			continue
		}
		lic.CanCustomWatermark = ccw == 1
		if lic.ExpiresAt.After(now) {
			result = append(result, &lic)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite list active user licenses rows: %w", err)
	}
	sortActiveLicenses(result)
	return result, nil
}

func (s *SQLiteClient) GetUserLicenseByOwnerAndMaster(ctx context.Context, ownerChatID int64, masterToken string) (*UserLicense, error) {
	query := `SELECT token_sha, master_token, owner_chat_id, userbot_id, tier, can_custom_watermark, expires_at, created_at, status
		FROM user_licenses WHERE (owner_chat_id = ? OR userbot_id = ?) AND master_token = ?
		ORDER BY created_at DESC LIMIT 1;`
	var ul UserLicense
	var canWM int
	err := s.DB.QueryRowContext(ctx, query, ownerChatID, ownerChatID, masterToken).Scan(
		&ul.TokenSHA, &ul.MasterToken, &ul.OwnerChatID, &ul.UserbotID, &ul.Tier, &canWM, &ul.ExpiresAt, &ul.CreatedAt, &ul.Status,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite get user license by owner and master: %w", err)
	}
	ul.CanCustomWatermark = canWM == 1
	return &ul, nil
}
