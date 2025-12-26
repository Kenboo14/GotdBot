package broadcast

import (
	"assterisk/ubot/applications/databases"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"context"
	"time"
)

type Models struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	Uid       int64              `bson:"uid"`
	Username  string             `bson:"username"`
	Status    bool               `bson:"status"`
	Role      string             `bson:"role"`
	CreatedAt time.Time          `bson:"created_at"`
}

type Repository struct {
	collection *mongo.Collection
	redis      *databases.Redis
}

func NewRepository(mongo *mongo.Database, redis *databases.Redis) *Repository {
	return &Repository{
		collection: mongo.Collection("users"),
		redis:      redis,
	}
}

func (r *Repository) GetAll(c context.Context) ([]Models, error) {
	ctx, cancel := context.WithTimeout(c, 5*time.Second)
	defer cancel()

	cursor, err := r.collection.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var res []Models
	if err := cursor.All(ctx, &res); err != nil {
		return nil, err
	}

	return res, nil
}

func (r *Repository) GetByID(c context.Context, id string) (*Models, error) {
	ctx, cancel := context.WithTimeout(c, 5*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	var res Models
	err = r.collection.FindOne(ctx, bson.M{"_id": objID}).Decode(&res)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	return &res, err
}

func (r *Repository) Create(c context.Context, user *Models) error {
	ctx, cancel := context.WithTimeout(c, 5*time.Second)
	defer cancel()

	_, err := r.collection.InsertOne(ctx, user)
	return err
}

func (r *Repository) UpdateStatus(c context.Context, id string, status string) error {
	ctx, cancel := context.WithTimeout(c, 5*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	_, err = r.collection.UpdateOne(
		ctx,
		bson.M{"_id": objID},
		bson.M{"$set": bson.M{"status": status}},
	)
	return err
}

func (r *Repository) Delete(c context.Context, id string) error {
	ctx, cancel := context.WithTimeout(c, 5*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	_, err = r.collection.DeleteOne(ctx, bson.M{"_id": objID})
	return err
}
