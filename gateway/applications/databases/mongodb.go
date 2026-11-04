package databases

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
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

func (m *MongoClient) SaveConfig(ctx context.Context, c *UserConfig) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("userbot_configs")
	opts := options.Update().SetUpsert(true)
	filter := bson.M{"_id": c.OwnerID}
	update := bson.M{"$set": c}
	_, err := col.UpdateOne(ctx, filter, update, opts)
	return err
}

func (m *MongoClient) GetConfig(ctx context.Context, ownerID int64) (*UserConfig, error) {
	if m == nil || m.Database == nil {
		return nil, nil
	}
	col := m.Database.Collection("userbot_configs")
	var c UserConfig
	err := col.FindOne(ctx, bson.M{"_id": ownerID}).Decode(&c)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (m *MongoClient) AddList(ctx context.Context, list *UserList) (string, error) {
	if m == nil || m.Database == nil {
		return "", nil
	}
	col := m.Database.Collection("userbot_lists")
	doc := bson.M{
		"owner_id":   list.OwnerID,
		"text":       list.Text,
		"media_type": list.MediaType,
		"created_at": list.CreatedAt,
	}
	res, err := col.InsertOne(ctx, doc)
	if err != nil {
		return "", err
	}
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		return oid.Hex(), nil
	}
	return "", nil
}

func (m *MongoClient) GetLists(ctx context.Context, ownerID int64) ([]UserList, error) {
	if m == nil || m.Database == nil {
		return nil, nil
	}
	col := m.Database.Collection("userbot_lists")
	cur, err := col.Find(ctx, bson.M{"owner_id": ownerID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var result []UserList
	for cur.Next(ctx) {
		var raw struct {
			ID        primitive.ObjectID `bson:"_id"`
			OwnerID   int64              `bson:"owner_id"`
			Text      string             `bson:"text"`
			MediaType string             `bson:"media_type"`
			CreatedAt time.Time          `bson:"created_at"`
		}
		if err := cur.Decode(&raw); err == nil {
			result = append(result, UserList{
				MongoID:   raw.ID.Hex(),
				OwnerID:   raw.OwnerID,
				Text:      raw.Text,
				MediaType: raw.MediaType,
				CreatedAt: raw.CreatedAt,
			})
		}
	}
	return result, nil
}

func (m *MongoClient) DeleteList(ctx context.Context, ownerID int64, mongoID string) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("userbot_lists")
	oid, err := primitive.ObjectIDFromHex(mongoID)
	if err != nil {
		return err
	}
	_, err = col.DeleteOne(ctx, bson.M{"_id": oid, "owner_id": ownerID})
	return err
}

func (m *MongoClient) ClearLists(ctx context.Context, ownerID int64) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("userbot_lists")
	_, err := col.DeleteMany(ctx, bson.M{"owner_id": ownerID})
	return err
}

func (m *MongoClient) AddGroup(ctx context.Context, g *UserGroup) (string, error) {
	if m == nil || m.Database == nil {
		return "", nil
	}
	col := m.Database.Collection("userbot_groups")
	doc := bson.M{
		"owner_id":   g.OwnerID,
		"target":     g.Target,
		"title":      g.Title,
		"chat_id":    g.ChatID,
		"created_at": g.CreatedAt,
	}
	res, err := col.InsertOne(ctx, doc)
	if err != nil {
		return "", err
	}
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		return oid.Hex(), nil
	}
	return "", nil
}

func (m *MongoClient) GetGroups(ctx context.Context, ownerID int64) ([]UserGroup, error) {
	if m == nil || m.Database == nil {
		return nil, nil
	}
	col := m.Database.Collection("userbot_groups")
	cur, err := col.Find(ctx, bson.M{"owner_id": ownerID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var result []UserGroup
	for cur.Next(ctx) {
		var raw struct {
			ID        primitive.ObjectID `bson:"_id"`
			OwnerID   int64              `bson:"owner_id"`
			Target    string             `bson:"target"`
			Title     string             `bson:"title"`
			ChatID    int64              `bson:"chat_id"`
			CreatedAt time.Time          `bson:"created_at"`
		}
		if err := cur.Decode(&raw); err == nil {
			result = append(result, UserGroup{
				MongoID:   raw.ID.Hex(),
				OwnerID:   raw.OwnerID,
				Target:    raw.Target,
				Title:     raw.Title,
				ChatID:    raw.ChatID,
				CreatedAt: raw.CreatedAt,
			})
		}
	}
	return result, nil
}

func (m *MongoClient) DeleteGroup(ctx context.Context, ownerID int64, mongoID string) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("userbot_groups")
	oid, err := primitive.ObjectIDFromHex(mongoID)
	if err != nil {
		return err
	}
	_, err = col.DeleteOne(ctx, bson.M{"_id": oid, "owner_id": ownerID})
	return err
}

func (m *MongoClient) ClearGroups(ctx context.Context, ownerID int64) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("userbot_groups")
	_, err := col.DeleteMany(ctx, bson.M{"owner_id": ownerID})
	return err
}

func (m *MongoClient) AddTopic(ctx context.Context, t *UserTopic) (string, error) {
	if m == nil || m.Database == nil {
		return "", nil
	}
	col := m.Database.Collection("userbot_topics")
	doc := bson.M{
		"owner_id":   t.OwnerID,
		"chat_id":    t.ChatID,
		"topic_id":   t.TopicID,
		"title":      t.Title,
		"created_at": t.CreatedAt,
	}
	res, err := col.InsertOne(ctx, doc)
	if err != nil {
		return "", err
	}
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		return oid.Hex(), nil
	}
	return "", nil
}

func (m *MongoClient) GetTopics(ctx context.Context, ownerID int64) ([]UserTopic, error) {
	if m == nil || m.Database == nil {
		return nil, nil
	}
	col := m.Database.Collection("userbot_topics")
	cur, err := col.Find(ctx, bson.M{"owner_id": ownerID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var result []UserTopic
	for cur.Next(ctx) {
		var raw struct {
			ID        primitive.ObjectID `bson:"_id"`
			OwnerID   int64              `bson:"owner_id"`
			ChatID    int64              `bson:"chat_id"`
			TopicID   int                `bson:"topic_id"`
			Title     string             `bson:"title"`
			CreatedAt time.Time          `bson:"created_at"`
		}
		if err := cur.Decode(&raw); err == nil {
			result = append(result, UserTopic{
				MongoID:   raw.ID.Hex(),
				OwnerID:   raw.OwnerID,
				ChatID:    raw.ChatID,
				TopicID:   raw.TopicID,
				Title:     raw.Title,
				CreatedAt: raw.CreatedAt,
			})
		}
	}
	return result, nil
}

func (m *MongoClient) ClearTopics(ctx context.Context, ownerID int64) error {
	if m == nil || m.Database == nil {
		return nil
	}
	col := m.Database.Collection("userbot_topics")
	_, err := col.DeleteMany(ctx, bson.M{"owner_id": ownerID})
	return err
}

