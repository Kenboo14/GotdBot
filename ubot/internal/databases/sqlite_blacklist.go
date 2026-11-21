package databases

import (
	"context"
	"fmt"
)

func (s *SQLiteClient) AddBlacklist(ctx context.Context, ownerID int64, chatType string, chatID int64) error {
	return s.UpsertBlacklist(ctx, BlacklistRecord{
		OwnerID:  ownerID,
		ChatType: chatType,
		ChatID:   chatID,
	})
}

func (s *SQLiteClient) UpsertBlacklist(ctx context.Context, rec BlacklistRecord) error {
	res, err := s.DB.ExecContext(
		ctx,
		`UPDATE broadcast_blacklist
		SET peer_kind = CASE WHEN ? != '' THEN ? ELSE peer_kind END,
			title = CASE WHEN ? != '' THEN ? ELSE title END,
			username = CASE WHEN ? != '' THEN ? ELSE username END
		WHERE owner_id = ? AND chat_type = ? AND chat_id = ?;`,
		rec.PeerKind, rec.PeerKind,
		rec.Title, rec.Title,
		rec.Username, rec.Username,
		rec.OwnerID, rec.ChatType, rec.ChatID,
	)
	if err != nil {
		return fmt.Errorf("update broadcast_blacklist: %w", err)
	}
	rows, err := res.RowsAffected()
	if err == nil && rows > 0 {
		return nil
	}
	_, err = s.DB.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO broadcast_blacklist (owner_id, chat_type, chat_id, peer_kind, title, username) VALUES (?, ?, ?, ?, ?, ?);`,
		rec.OwnerID, rec.ChatType, rec.ChatID, rec.PeerKind, rec.Title, rec.Username,
	)
	if err != nil {
		return fmt.Errorf("insert broadcast_blacklist: %w", err)
	}
	return nil
}

func (s *SQLiteClient) RemoveBlacklist(ctx context.Context, ownerID int64, chatType string, chatID int64) error {
	query := `DELETE FROM broadcast_blacklist WHERE owner_id = ? AND chat_type = ? AND chat_id = ?;`
	if _, err := s.DB.ExecContext(ctx, query, ownerID, chatType, chatID); err != nil {
		return fmt.Errorf("delete broadcast_blacklist: %w", err)
	}
	return nil
}

func (s *SQLiteClient) RemoveBlacklistAny(ctx context.Context, ownerID int64, chatType string, chatIDs []int64) error {
	for _, id := range chatIDs {
		if id == 0 {
			continue
		}
		if err := s.RemoveBlacklist(ctx, ownerID, chatType, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteClient) ClearBlacklist(ctx context.Context, ownerID int64) error {
	query := `DELETE FROM broadcast_blacklist WHERE owner_id = ?;`
	if _, err := s.DB.ExecContext(ctx, query, ownerID); err != nil {
		return fmt.Errorf("clear broadcast_blacklist: %w", err)
	}
	return nil
}

func (s *SQLiteClient) ListBlacklist(ctx context.Context, ownerID int64) ([]BlacklistRecord, error) {
	query := `SELECT owner_id, chat_type, chat_id, COALESCE(peer_kind, ''), COALESCE(title, ''), COALESCE(username, ''), created_at
		FROM broadcast_blacklist WHERE owner_id = ? ORDER BY created_at ASC, rowid ASC;`
	rows, err := s.DB.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list broadcast_blacklist: %w", err)
	}
	defer rows.Close()

	var result []BlacklistRecord
	for rows.Next() {
		var r BlacklistRecord
		if err := rows.Scan(&r.OwnerID, &r.ChatType, &r.ChatID, &r.PeerKind, &r.Title, &r.Username, &r.CreatedAt); err != nil {
			continue
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *SQLiteClient) IsBlacklisted(ctx context.Context, ownerID int64, chatType string, chatID int64) (bool, error) {
	var count int
	query := `SELECT COUNT(1) FROM broadcast_blacklist WHERE owner_id = ? AND chat_type = ? AND chat_id = ?;`
	err := s.DB.QueryRowContext(ctx, query, ownerID, chatType, chatID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check broadcast_blacklist: %w", err)
	}
	return count > 0, nil
}
