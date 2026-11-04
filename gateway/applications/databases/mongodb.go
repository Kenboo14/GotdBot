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

	opts := options.Client().ApplyURI(uri)
	opts.SetServerSelectionTimeout(8 * time.Second)
	opts.SetConnectTimeout(8 * time.Second)

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect mongodb: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		return nil, fmt.Errorf("failed to ping mongodb: %w", err)
	}

	db := client.Database(dbName)
	log.Printf("🍃 Gateway MongoDB connected: %s (db: %s)", uri[:min(len(uri), 25)]+"...", dbName)
	return &MongoClient{Client: client, Database: db}, nil
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
