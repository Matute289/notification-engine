package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const collectionTemplates = "notification_templates"

// templateDoc is the MongoDB wire representation. _id stores the UUID string
// so the domain uuid.UUID type is preserved without ObjectID translation.
type templateDoc struct {
	ID          string    `bson:"_id"`
	Name        string    `bson:"name"`
	Channel     string    `bson:"channel"`
	Locale      string    `bson:"locale"`
	Subject     string    `bson:"subject"`
	Body        string    `bson:"body"`
	MediaURLs   []string  `bson:"media_urls,omitempty"`
	Version     int       `bson:"version"`
	OwnerUserID int64     `bson:"owner_user_id"`
	CreatedAt   time.Time `bson:"created_at"`
	UpdatedAt   time.Time `bson:"updated_at"`
}

// TemplateRepository implements port.TemplateRepository against MongoDB.
type TemplateRepository struct {
	col *mongo.Collection
}

// NewTemplateRepository creates the repository and ensures a unique index on
// (owner_user_id, name, channel, locale, version).
func NewTemplateRepository(db *mongo.Database) (*TemplateRepository, error) {
	col := db.Collection(collectionTemplates)
	idx := mongo.IndexModel{
		Keys: bson.D{
			{Key: "owner_user_id", Value: 1},
			{Key: "name", Value: 1},
			{Key: "channel", Value: 1},
			{Key: "locale", Value: 1},
			{Key: "version", Value: 1},
		},
		Options: options.Index().SetUnique(true).SetName("owner_user_id_name_channel_locale_version"),
	}
	if _, err := col.Indexes().CreateOne(context.Background(), idx); err != nil {
		return nil, fmt.Errorf("mongodb: ensure template index: %w", err)
	}
	return &TemplateRepository{col: col}, nil
}

var _ port.TemplateRepository = (*TemplateRepository)(nil)

func (r *TemplateRepository) Create(ctx context.Context, t domain.Template) error {
	doc := templateDoc{
		ID:          t.ID.String(),
		Name:        t.Name,
		Channel:     string(t.Channel),
		Locale:      t.Locale,
		Subject:     t.Subject,
		Body:        t.Body,
		MediaURLs:   t.MediaURLs,
		Version:     t.Version,
		OwnerUserID: t.OwnerUserID,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
	if _, err := r.col.InsertOne(ctx, doc); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("%w: template (name=%s channel=%s locale=%s version=%d) already exists",
				domain.ErrAlreadyExists, t.Name, t.Channel, t.Locale, t.Version)
		}
		return fmt.Errorf("mongodb: create template: %w", err)
	}
	return nil
}

func (r *TemplateRepository) Get(ctx context.Context, id uuid.UUID) (domain.Template, error) {
	var doc templateDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id.String()}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return domain.Template{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Template{}, fmt.Errorf("mongodb: get template: %w", err)
	}
	return domain.Template{
		ID:          uuid.MustParse(doc.ID),
		Name:        doc.Name,
		Channel:     domain.Channel(doc.Channel),
		Locale:      doc.Locale,
		Subject:     doc.Subject,
		Body:        doc.Body,
		MediaURLs:   doc.MediaURLs,
		Version:     doc.Version,
		OwnerUserID: doc.OwnerUserID,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}, nil
}

func (r *TemplateRepository) Update(ctx context.Context, t domain.Template) error {
	filter := bson.M{"_id": t.ID.String()}
	update := bson.M{"$set": bson.M{
		"name":       t.Name,
		"subject":    t.Subject,
		"body":       t.Body,
		"media_urls": t.MediaURLs,
		"updated_at": t.UpdatedAt,
	}}
	res, err := r.col.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("mongodb: update template: %w", err)
	}
	if res.MatchedCount == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *TemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.col.DeleteOne(ctx, bson.M{"_id": id.String()})
	if err != nil {
		return fmt.Errorf("mongodb: delete template: %w", err)
	}
	if res.DeletedCount == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *TemplateRepository) List(ctx context.Context, ownerUserID int64, channel *domain.Channel) ([]domain.Template, error) {
	filter := bson.M{"owner_user_id": ownerUserID}
	if channel != nil {
		filter["channel"] = string(*channel)
	}
	opts := options.Find().SetSort(bson.D{{Key: "channel", Value: 1}, {Key: "name", Value: 1}})
	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("mongodb: list templates: %w", err)
	}
	defer cursor.Close(ctx)
	var out []domain.Template
	for cursor.Next(ctx) {
		var doc templateDoc
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("mongodb: list templates decode: %w", err)
		}
		out = append(out, domain.Template{
			ID:          uuid.MustParse(doc.ID),
			Name:        doc.Name,
			Channel:     domain.Channel(doc.Channel),
			Locale:      doc.Locale,
			Subject:     doc.Subject,
			Body:        doc.Body,
			MediaURLs:   doc.MediaURLs,
			Version:     doc.Version,
			OwnerUserID: doc.OwnerUserID,
			CreatedAt:   doc.CreatedAt,
			UpdatedAt:   doc.UpdatedAt,
		})
	}
	return out, cursor.Err()
}
