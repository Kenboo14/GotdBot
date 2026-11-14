package userbot

import (
	"assterisk/ubot/internal/databases"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/gotd/td/session"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type sqliteStorage struct {
	sqlite  *databases.SQLiteClient
	db      *mongo.Database // optional remote backup
	chatID  int64
	ownerID int64
	key     []byte
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
	if s.sqlite != nil {
		row, err := s.sqlite.GetSessionByChat(ctx, s.chatID)
		if err == nil && row != nil {
			_ = s.sqlite.SaveSession(ctx, s.chatID, row.OwnerID, id, row.Blob, row.Prefix, row.PrefixSet)
		}
	}
	if s.db != nil {
		go func(cid, accID int64) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_sessions").UpdateByID(bgCtx, cid, bson.M{
				"$set": bson.M{"account_id": accID, "updated_at": time.Now().UTC()},
			})
		}(s.chatID, id)
	}
	return nil
}

func (s *sqliteStorage) setPrefix(ctx context.Context, prefix string, configured bool) error {
	if s.sqlite != nil {
		if err := s.sqlite.UpdatePrefix(ctx, s.chatID, prefix); err != nil {
			return err
		}
	}
	if s.db != nil {
		go func(cid int64, pfx string, conf bool) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.db.Collection("userbot_sessions").UpdateByID(bgCtx, cid, bson.M{
				"$set": bson.M{"prefix": pfx, "prefix_configured": conf, "updated_at": time.Now().UTC()},
			})
		}(s.chatID, prefix, configured)
	}
	return nil
}

func encrypt(key, data []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(append(nonce, gcm.Seal(nil, nonce, data, nil)...)), nil
}

func decrypt(key []byte, input string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("session terenkripsi tidak valid")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
}
