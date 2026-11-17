package userbot

import (
	"assterisk/ubot/internal/databases"
	"context"
	"time"

	gobotsession "github.com/Kenboo14/GoBot/session"
	"github.com/gotd/td/session"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type sqliteStorage struct {
	sqlite    *databases.SQLiteClient
	db        *mongo.Database // optional remote backup
	chatID    int64
	ownerID   int64
	accountID int64
	key       []byte
}

type settingsRecord struct {
	ChatID    int64     `bson:"_id"`
	OwnerID   int64     `bson:"owner_id,omitempty"`
	AccountID int64     `bson:"account_id,omitempty"`
	Prefix    string    `bson:"prefix,omitempty"`
	PrefixSet bool      `bson:"prefix_configured"`
	UpdatedAt time.Time `bson:"updated_at"`
}

func (s *sqliteStorage) LoadSession(ctx context.Context) ([]byte, error) {
	if s.sqlite != nil {
		row, err := s.sqlite.GetSessionByChat(ctx, s.chatID)
		if err == nil && row != nil && row.Blob != "" {
			return decrypt(s.key, row.Blob)
		}
	}
	// Fallback ke MongoDB Cloud Atlas jika sesi belum ada di SQLite lokal
	if s.db != nil {
		var mRow record
		err := s.db.Collection("userbot_sessions").FindOne(ctx, bson.M{
			"$or": []bson.M{
				{"_id": s.chatID},
				{"owner_id": s.ownerID},
			},
			"session": bson.M{"$ne": ""},
		}).Decode(&mRow)
		if err == nil && mRow.Blob != "" {
			accID := mRow.AccountID
			if accID == 0 {
				accID = mRow.OwnerID
			}
			if accID == 0 {
				accID = mRow.ChatID
			}
			if s.sqlite != nil {
				_ = s.sqlite.SaveSession(ctx, mRow.ChatID, mRow.OwnerID, accID, mRow.Blob, mRow.Prefix, mRow.PrefixSet)
			}
			return decrypt(s.key, mRow.Blob)
		}
	}
	return nil, session.ErrNotFound
}

func (s *sqliteStorage) StoreSession(ctx context.Context, data []byte) error {
	blob, err := encrypt(s.key, data)
	if err != nil {
		return err
	}
	if s.sqlite != nil {
		if err := s.sqlite.SaveSession(ctx, s.chatID, s.ownerID, 0, blob, "!", false); err != nil {
			return err
		}
	}
	// Asynchronous offsite backup to MongoDB
	if s.db != nil {
		go func(cid, oid int64, b string) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_sessions").UpdateByID(bgCtx, cid, bson.M{
				"$set": bson.M{"owner_id": oid, "session": b, "updated_at": time.Now().UTC()},
			}, options.Update().SetUpsert(true))
		}(s.chatID, s.ownerID, blob)
	}
	return nil
}

func (s *sqliteStorage) setAccount(ctx context.Context, id int64) error {
	s.accountID = id
	if s.sqlite != nil {
		row, err := s.sqlite.GetSessionByChat(ctx, s.chatID)
		if err == nil && row != nil {
			_ = s.sqlite.SaveSession(ctx, s.chatID, row.OwnerID, id, row.Blob, row.Prefix, row.PrefixSet)
		}
		_ = s.sqlite.SaveUserbotPrefix(ctx, s.chatID, s.ownerID, id, "!", false)
	}
	if s.db != nil {
		go func(cid, oid, accID int64) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_sessions").UpdateByID(bgCtx, cid, bson.M{
				"$set": bson.M{"account_id": accID, "updated_at": time.Now().UTC()},
			})
			_, _ = s.db.Collection("userbot_settings").UpdateByID(bgCtx, cid, bson.M{
				"$set": bson.M{"owner_id": oid, "account_id": accID, "updated_at": time.Now().UTC()},
			}, options.Update().SetUpsert(true))
		}(s.chatID, s.ownerID, id)
	}
	return nil
}

func (s *sqliteStorage) loadUserbotPrefix(ctx context.Context, accountID int64) (string, bool) {
	if s.sqlite != nil {
		if st, err := s.sqlite.GetUserbotSettings(ctx, s.chatID); err == nil && st != nil && st.PrefixSet {
			return st.Prefix, true
		}
		if accountID > 0 {
			if st, err := s.sqlite.GetUserbotSettingsByAccount(ctx, accountID); err == nil && st != nil && st.PrefixSet {
				return st.Prefix, true
			}
		}
		if row, err := s.sqlite.GetSessionByChat(ctx, s.chatID); err == nil && row != nil && row.PrefixSet {
			return row.Prefix, true
		}
	}
	if s.db != nil {
		var mSet settingsRecord
		filter := bson.M{"_id": s.chatID, "prefix_configured": true}
		if err := s.db.Collection("userbot_settings").FindOne(ctx, filter).Decode(&mSet); err == nil && mSet.PrefixSet {
			if s.sqlite != nil {
				_ = s.sqlite.SaveUserbotPrefix(ctx, s.chatID, s.ownerID, accountID, mSet.Prefix, true)
			}
			return mSet.Prefix, true
		}
		if accountID > 0 {
			if err := s.db.Collection("userbot_settings").FindOne(ctx, bson.M{"account_id": accountID, "prefix_configured": true}).Decode(&mSet); err == nil && mSet.PrefixSet {
				if s.sqlite != nil {
					_ = s.sqlite.SaveUserbotPrefix(ctx, s.chatID, s.ownerID, accountID, mSet.Prefix, true)
				}
				return mSet.Prefix, true
			}
		}
		var mRow record
		if err := s.db.Collection("userbot_sessions").FindOne(ctx, bson.M{"_id": s.chatID, "prefix_configured": true}).Decode(&mRow); err == nil && mRow.PrefixSet {
			if s.sqlite != nil {
				_ = s.sqlite.SaveUserbotPrefix(ctx, s.chatID, s.ownerID, accountID, mRow.Prefix, true)
			}
			return mRow.Prefix, true
		}
	}
	return "!", false
}

func (s *sqliteStorage) setPrefix(ctx context.Context, prefix string, configured bool) error {
	accID := s.accountID
	ownerID := s.ownerID
	if ownerID == 0 {
		ownerID = s.chatID
	}
	if s.sqlite != nil {
		if accID == 0 {
			if row, err := s.sqlite.GetSessionByChat(ctx, s.chatID); err == nil && row != nil {
				accID = row.AccountID
			}
		}
		if err := s.sqlite.SaveUserbotPrefix(ctx, s.chatID, ownerID, accID, prefix, configured); err != nil {
			return err
		}
		if configured {
			if err := s.sqlite.UpdatePrefix(ctx, s.chatID, prefix); err != nil {
				return err
			}
		}
	}
	if s.db != nil {
		go func(cid, oid, aid int64, pfx string, conf bool) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			setFields := bson.M{
				"owner_id":          oid,
				"prefix":            pfx,
				"prefix_configured": conf,
				"updated_at":        time.Now().UTC(),
			}
			if aid > 0 {
				setFields["account_id"] = aid
			}
			_, _ = s.db.Collection("userbot_settings").UpdateByID(bgCtx, cid, bson.M{
				"$set": setFields,
			}, options.Update().SetUpsert(true))
			_, _ = s.db.Collection("userbot_sessions").UpdateByID(bgCtx, cid, bson.M{
				"$set": bson.M{"prefix": pfx, "prefix_configured": conf, "updated_at": time.Now().UTC()},
			})
		}(s.chatID, ownerID, accID, prefix, configured)
	}
	return nil
}

func encrypt(key, data []byte) (string, error) {
	return gobotsession.Encrypt(key, data)
}

func decrypt(key []byte, input string) ([]byte, error) {
	return gobotsession.Decrypt(key, input)
}
