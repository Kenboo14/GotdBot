package databases

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoClient struct {
	Client   *mongo.Client
	Database *mongo.Database
}

func NewMongoClient(ctx context.Context, uri, dbName string) (*MongoClient, error) {
	if uri == "" {
		return nil, fmt.Errorf("empty mongodb uri")
	}
	if dbName == "" {
		dbName = "gotdbot"
	}

	const baseDelay = 3 * time.Second
	const maxDelay = 30 * time.Second
	const perAttemptTimeout = 10 * time.Second

	opts := options.Client().ApplyURI(uri)
	opts.SetServerSelectionTimeout(8 * time.Second)
	opts.SetConnectTimeout(8 * time.Second)

	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("mongodb context cancelled: %w", ctx.Err())
		}

		attemptCtx, cancel := context.WithTimeout(ctx, perAttemptTimeout)
		client, err := mongo.Connect(attemptCtx, opts)
		if err != nil {
			cancel()
			delay := time.Duration(attempt) * baseDelay
			if delay > maxDelay {
				delay = maxDelay
			}
			log.Printf("⚠️ MongoDB connect attempt %d failed: %v. Retrying in %v...", attempt, err, delay)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("mongodb context cancelled: %w", ctx.Err())
			case <-time.After(delay):
			}
			continue
		}

		pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
		pingErr := client.Ping(pingCtx, nil)
		pingCancel()
		cancel()

		if pingErr != nil {
			_ = client.Disconnect(context.Background())
			delay := time.Duration(attempt) * baseDelay
			if delay > maxDelay {
				delay = maxDelay
			}
			log.Printf("⚠️ MongoDB ping attempt %d failed: %v. Retrying in %v...", attempt, pingErr, delay)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("mongodb context cancelled: %w", ctx.Err())
			case <-time.After(delay):
			}
			continue
		}

		db := client.Database(dbName)
		log.Printf("🍃 Gateway MongoDB connected (attempt %d): %s (db: %s)", attempt, uri[:min(len(uri), 25)]+"...", dbName)
		mc := &MongoClient{Client: client, Database: db}
		mc.ensureIndexes(context.Background())
		return mc, nil
	}
}

func (m *MongoClient) ensureIndexes(ctx context.Context) {
	if m == nil || m.Database == nil {
		return
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	colLic := m.Database.Collection("user_licenses")
	_, _ = colLic.Indexes().CreateMany(ctxTimeout, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "owner_chat_id", Value: 1}, {Key: "status", Value: 1}, {Key: "expires_at", Value: -1}},
			Options: options.Index().SetName("idx_licenses_owner_status"),
		},
		{
			Keys:    bson.D{{Key: "userbot_id", Value: 1}, {Key: "status", Value: 1}, {Key: "expires_at", Value: -1}},
			Options: options.Index().SetName("idx_licenses_userbot_status"),
		},
	})

	colTokens := m.Database.Collection("tokens_master")
	_, _ = colTokens.Indexes().CreateMany(ctxTimeout, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "token", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("idx_tokens_token_unique"),
		},
		{
			Keys:    bson.D{{Key: "is_shared", Value: 1}, {Key: "status", Value: 1}},
			Options: options.Index().SetName("idx_tokens_shared_status"),
		},
	})

	colCfg := m.Database.Collection("jaseb_configurasi")
	_, _ = colCfg.Indexes().CreateMany(ctxTimeout, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "owner_id", Value: 1}},
			Options: options.Index().SetName("idx_jaseb_owner_id"),
		},
	})

	log.Println("🍃 Gateway MongoDB indexes ensured (licenses, tokens, config).")
}

func (m *MongoClient) HasSession(ctx context.Context, ownerID int64) (bool, error) {
	if m == nil || m.Database == nil {
		return false, nil
	}
	col := m.Database.Collection("userbot_sessions")
	count, err := col.CountDocuments(ctx, bson.M{
		"$or": []bson.M{
			{"_id": ownerID},
			{"owner_id": ownerID},
			{"account_id": ownerID},
		},
		"session": bson.M{"$ne": ""},
	})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (m *MongoClient) SaveConfig(ctx context.Context, c *JasebConfig) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("jaseb_configurasi")
	opts := options.Update().SetUpsert(true)
	filter := bson.M{"_id": c.OwnerID}
	update := bson.M{"$set": c}
	_, err := col.UpdateOne(ctx, filter, update, opts)
	return err
}

func (m *MongoClient) GetConfig(ctx context.Context, ownerID int64) (*JasebConfig, error) {
	if m == nil || m.Database == nil {
		return nil, nil
	}
	col := m.Database.Collection("jaseb_configurasi")
	var c JasebConfig
	err := col.FindOne(ctx, bson.M{"_id": ownerID}).Decode(&c)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (m *MongoClient) AddListCacheID(ctx context.Context, ownerID int64, cacheMsgID int) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("jaseb_configurasi")
	opts := options.Update().SetUpsert(true)
	filter := bson.M{"_id": ownerID}
	update := bson.M{
		"$push": bson.M{"lists": cacheMsgID},
		"$set":  bson.M{"updated_at": time.Now()},
	}
	_, err := col.UpdateOne(ctx, filter, update, opts)
	return err
}

func (m *MongoClient) DeleteListCacheID(ctx context.Context, ownerID int64, cacheMsgID int) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("jaseb_configurasi")
	filter := bson.M{"_id": ownerID}
	update := bson.M{
		"$pull": bson.M{"lists": cacheMsgID},
		"$set":  bson.M{"updated_at": time.Now()},
	}
	_, err := col.UpdateOne(ctx, filter, update)
	return err
}

func (m *MongoClient) ClearLists(ctx context.Context, ownerID int64) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("jaseb_configurasi")
	filter := bson.M{"_id": ownerID}
	update := bson.M{
		"$set": bson.M{
			"lists":      []int{},
			"updated_at": time.Now(),
		},
	}
	_, err := col.UpdateOne(ctx, filter, update)
	return err
}

func (m *MongoClient) AddGroup(ctx context.Context, ownerID int64, g GroupEntry) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("jaseb_configurasi")
	opts := options.Update().SetUpsert(true)
	filter := bson.M{"_id": ownerID}
	update := bson.M{
		"$push": bson.M{"groups": g},
		"$set":  bson.M{"updated_at": time.Now()},
	}
	_, err := col.UpdateOne(ctx, filter, update, opts)
	return err
}

func (m *MongoClient) DeleteGroup(ctx context.Context, ownerID int64, target string) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("jaseb_configurasi")
	filter := bson.M{"_id": ownerID}
	update := bson.M{
		"$pull": bson.M{"groups": bson.M{"target": target}},
		"$set":  bson.M{"updated_at": time.Now()},
	}
	_, err := col.UpdateOne(ctx, filter, update)
	return err
}

func (m *MongoClient) ClearGroups(ctx context.Context, ownerID int64) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("jaseb_configurasi")
	filter := bson.M{"_id": ownerID}
	update := bson.M{
		"$set": bson.M{
			"groups":     []GroupEntry{},
			"updated_at": time.Now(),
		},
	}
	_, err := col.UpdateOne(ctx, filter, update)
	return err
}

func (m *MongoClient) AddTopic(ctx context.Context, ownerID int64, t TopicEntry) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("jaseb_configurasi")
	opts := options.Update().SetUpsert(true)
	filter := bson.M{"_id": ownerID}
	update := bson.M{
		"$push": bson.M{"topics": t},
		"$set":  bson.M{"updated_at": time.Now()},
	}
	_, err := col.UpdateOne(ctx, filter, update, opts)
	return err
}

func (m *MongoClient) ClearTopics(ctx context.Context, ownerID int64) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("jaseb_configurasi")
	filter := bson.M{"_id": ownerID}
	update := bson.M{
		"$set": bson.M{
			"topics":     []TopicEntry{},
			"updated_at": time.Now(),
		},
	}
	_, err := col.UpdateOne(ctx, filter, update)
	return err
}

func (m *MongoClient) SaveTokenMaster(ctx context.Context, tm *TokenMaster) error {
	if m == nil || m.Database == nil || tm == nil {
		return nil
	}
	col := m.Database.Collection("tokens_master")
	opts := options.Update().SetUpsert(true)
	filter := bson.M{"_id": tm.Token}
	update := bson.M{"$set": tm}
	_, err := col.UpdateOne(ctx, filter, update, opts)
	return err
}

func (m *MongoClient) GetTokenMaster(ctx context.Context, token string) (*TokenMaster, error) {
	if m == nil || m.Database == nil {
		return nil, nil
	}
	col := m.Database.Collection("tokens_master")
	var tm TokenMaster
	err := col.FindOne(ctx, bson.M{"_id": token}).Decode(&tm)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tm, nil
}

func (m *MongoClient) GetActiveSharedFreeToken(ctx context.Context) (*TokenMaster, error) {
	if m == nil || m.Database == nil {
		return nil, nil
	}
	col := m.Database.Collection("tokens_master")
	filter := bson.M{
		"tier":      "Free",
		"is_shared": true,
		"status":    "active",
	}
	opts := options.FindOne().SetSort(bson.M{"created_at": -1})
	var tm TokenMaster
	err := col.FindOne(ctx, filter, opts).Decode(&tm)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tm, nil
}

func (m *MongoClient) UpdateTokenMasterDuration(ctx context.Context, token string, durationSeconds int64) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("tokens_master")
	filter := bson.M{"_id": token}
	update := bson.M{"$set": bson.M{"duration_seconds": durationSeconds}}
	_, err := col.UpdateOne(ctx, filter, update)
	return err
}

func (m *MongoClient) IncrementTokenMasterClaim(ctx context.Context, token string) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("tokens_master")
	filter := bson.M{"_id": token}
	update := bson.M{"$inc": bson.M{"claim_count": 1}}
	_, err := col.UpdateOne(ctx, filter, update)
	return err
}

func (m *MongoClient) SaveUserLicense(ctx context.Context, ul *UserLicense) error {
	if m == nil || m.Database == nil || ul == nil {
		return nil
	}
	col := m.Database.Collection("user_licenses")
	opts := options.Update().SetUpsert(true)
	filter := bson.M{"_id": ul.TokenSHA}
	update := bson.M{"$set": ul}
	_, err := col.UpdateOne(ctx, filter, update, opts)
	return err
}

func (m *MongoClient) GetUserLicenseBySHA(ctx context.Context, tokenSHA string) (*UserLicense, error) {
	if m == nil || m.Database == nil {
		return nil, nil
	}
	col := m.Database.Collection("user_licenses")
	var ul UserLicense
	err := col.FindOne(ctx, bson.M{"_id": tokenSHA}).Decode(&ul)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ul, nil
}

func (m *MongoClient) GetActiveUserLicense(ctx context.Context, ownerChatID int64) (*UserLicense, error) {
	if m == nil || m.Database == nil {
		return nil, nil
	}
	col := m.Database.Collection("user_licenses")
	filter := bson.M{
		"$or": []bson.M{
			{"owner_chat_id": ownerChatID},
			{"userbot_id": ownerChatID},
		},
		"status":     "active",
		"expires_at": bson.M{"$gt": time.Now()},
	}
	opts := options.FindOne().SetSort(bson.M{"expires_at": -1})
	var ul UserLicense
	err := col.FindOne(ctx, filter, opts).Decode(&ul)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ul, nil
}

func (m *MongoClient) RelinkUserData(ctx context.Context, oldOwnerID, newOwnerID int64) error {
	if m == nil || m.Database == nil {
		return nil
	}
	colLic := m.Database.Collection("user_licenses")
	_, err := colLic.UpdateMany(ctx,
		bson.M{"owner_chat_id": oldOwnerID},
		bson.M{"$set": bson.M{"owner_chat_id": newOwnerID}},
	)
	if err != nil {
		log.Printf("⚠️ Mongo RelinkUserData user_licenses err: %v", err)
	}

	colCfg := m.Database.Collection("jaseb_configurasi")
	var existing JasebConfig
	if errCfg := colCfg.FindOne(ctx, bson.M{"_id": oldOwnerID}).Decode(&existing); errCfg == nil {
		existing.OwnerID = newOwnerID
		existing.UpdatedAt = time.Now()
		opts := options.Update().SetUpsert(true)
		_, _ = colCfg.UpdateOne(ctx, bson.M{"_id": newOwnerID}, bson.M{"$set": existing}, opts)
		_, _ = colCfg.DeleteOne(ctx, bson.M{"_id": oldOwnerID})
	}
	return nil
}
