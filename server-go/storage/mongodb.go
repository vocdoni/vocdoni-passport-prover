package storage

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoDB struct {
	client   *mongo.Client
	database *mongo.Database
}

type Petition struct {
	ID              primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	PetitionID      string             `bson:"petition_id" json:"petitionId"`
	Name            string             `bson:"name" json:"name"`
	Purpose         string             `bson:"purpose" json:"purpose"`
	Scope           string             `bson:"scope" json:"scope"`
	Query           map[string]any     `bson:"query" json:"query"`
	Preset          string             `bson:"preset,omitempty" json:"preset,omitempty"`
	CreatedAt       time.Time          `bson:"created_at" json:"createdAt"`
	SignatureCount  int                `bson:"signature_count" json:"signatureCount"`
	DisclosedFields []string           `bson:"disclosed_fields" json:"disclosedFields"`
}

type Signature struct {
	ID              primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	PetitionID      string             `bson:"petition_id" json:"petitionId"`
	Nullifier       string             `bson:"nullifier" json:"nullifier"`
	SignerAddress   string             `bson:"signer_address,omitempty" json:"signerAddress,omitempty"`
	ProofHash       string             `bson:"proof_hash" json:"proofHash"`
	DisclosedFields []string           `bson:"disclosed_fields" json:"disclosedFields"`
	DisclosedValues map[string]string  `bson:"disclosed_values,omitempty" json:"disclosedValues,omitempty"`
	Timestamp       time.Time          `bson:"timestamp" json:"timestamp"`
	Version         string             `bson:"version" json:"version"`
}

func NewMongoDB(uri, database string) (*MongoDB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOpts := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("connect to mongodb: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("ping mongodb: %w", err)
	}

	db := client.Database(database)

	petitions := db.Collection("petitions")
	_, err = petitions.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "petition_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "created_at", Value: -1}}},
	})
	if err != nil {
		return nil, fmt.Errorf("create petitions indexes: %w", err)
	}

	signatures := db.Collection("signatures")
	_, err = signatures.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "petition_id", Value: 1}, {Key: "nullifier", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "petition_id", Value: 1}}},
		{Keys: bson.D{{Key: "timestamp", Value: -1}}},
	})
	if err != nil {
		return nil, fmt.Errorf("create signatures indexes: %w", err)
	}

	return &MongoDB{
		client:   client,
		database: db,
	}, nil
}

func (m *MongoDB) Close(ctx context.Context) error {
	return m.client.Disconnect(ctx)
}

func (m *MongoDB) CreatePetition(ctx context.Context, petition *Petition) error {
	if petition.PetitionID == "" {
		petition.PetitionID = primitive.NewObjectID().Hex()
	}
	petition.CreatedAt = time.Now()
	petition.SignatureCount = 0

	petition.DisclosedFields = collectDisclosedFields(petition.Query)

	_, err := m.database.Collection("petitions").InsertOne(ctx, petition)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("petition with ID %s already exists", petition.PetitionID)
		}
		return fmt.Errorf("insert petition: %w", err)
	}
	return nil
}

func (m *MongoDB) GetPetition(ctx context.Context, petitionID string) (*Petition, error) {
	var petition Petition
	err := m.database.Collection("petitions").FindOne(ctx, bson.M{"petition_id": petitionID}).Decode(&petition)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("find petition: %w", err)
	}
	return &petition, nil
}

func (m *MongoDB) ListPetitions(ctx context.Context, limit, offset int64) ([]*Petition, int64, error) {
	collection := m.database.Collection("petitions")

	total, err := collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, fmt.Errorf("count petitions: %w", err)
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(limit).
		SetSkip(offset)

	cursor, err := collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("find petitions: %w", err)
	}
	defer cursor.Close(ctx)

	var petitions []*Petition
	if err := cursor.All(ctx, &petitions); err != nil {
		return nil, 0, fmt.Errorf("decode petitions: %w", err)
	}

	return petitions, total, nil
}

func (m *MongoDB) SaveSignature(ctx context.Context, sig *Signature) error {
	sig.Timestamp = time.Now()

	_, err := m.database.Collection("signatures").InsertOne(ctx, sig)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("signature with nullifier %s already exists for petition %s", sig.Nullifier, sig.PetitionID)
		}
		return fmt.Errorf("insert signature: %w", err)
	}

	_, err = m.database.Collection("petitions").UpdateOne(
		ctx,
		bson.M{"petition_id": sig.PetitionID},
		bson.M{"$inc": bson.M{"signature_count": 1}},
	)
	if err != nil {
		return fmt.Errorf("increment signature count: %w", err)
	}

	return nil
}

func (m *MongoDB) GetSignaturesByPetition(ctx context.Context, petitionID string, limit, offset int64) ([]*Signature, int64, error) {
	collection := m.database.Collection("signatures")
	filter := bson.M{"petition_id": petitionID}

	total, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count signatures: %w", err)
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(limit).
		SetSkip(offset)

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("find signatures: %w", err)
	}
	defer cursor.Close(ctx)

	var signatures []*Signature
	if err := cursor.All(ctx, &signatures); err != nil {
		return nil, 0, fmt.Errorf("decode signatures: %w", err)
	}

	return signatures, total, nil
}

func (m *MongoDB) SignatureExists(ctx context.Context, petitionID, nullifier string) (bool, error) {
	count, err := m.database.Collection("signatures").CountDocuments(ctx, bson.M{
		"petition_id": petitionID,
		"nullifier":   nullifier,
	})
	if err != nil {
		return false, fmt.Errorf("count signature: %w", err)
	}
	return count > 0, nil
}

func collectDisclosedFields(query map[string]any) []string {
	if query == nil {
		return nil
	}
	var fields []string
	for key, value := range query {
		if m, ok := value.(map[string]any); ok {
			if disclose, ok := m["disclose"].(bool); ok && disclose {
				fields = append(fields, key)
			}
			if _, ok := m["eq"]; ok {
				fields = append(fields, key)
			}
		}
	}
	return fields
}
