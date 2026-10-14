package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

func TestIntegrationRelightPreintentReader(t *testing.T) {
	path := os.Getenv("PHOTO_TEST_RELIGHT_PREINTENT_SEED")
	if path == "" {
		t.Skip("isolated PostgreSQL/MinIO pre-intent seed required")
	}
	var seed struct {
		Tenant, Project, Snapshot, Actor, Session uuid.UUID
		Kind, Source, Original                    string
		OriginalID                                uuid.UUID `json:"original_id"`
		Params                                    RelightParams
		Full                                      bool
		External                                  bool
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &seed) != nil {
		t.Fatal("invalid seed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("database configuration")
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE photo_scheduler")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("scheduler pool")
	}
	defer pool.Close()
	admin, err := pgx.Connect(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("control connection")
	}
	defer admin.Close(context.Background())
	store, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET"), AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal("object store")
	}
	objects := &relightObservedObjects{store: store}
	reader := SQLRelightPreintentReader{DB: pool, Objects: objects, Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET")}
	who := auth.Identity{UserID: seed.Actor, SessionID: seed.Session}
	var sharedBudget *RelightManualRasterBudget
	load := func() (RelightPreintentDocuments, error) {
		if seed.Full {
			budget, err := NewRelightManualRasterBudget(RelightManualRasterLimits{PointChecks: 1000000, EdgeEvaluations: 1000000, PayloadBytes: 1000000})
			if err != nil {
				return RelightPreintentDocuments{}, err
			}
			if sharedBudget != nil {
				budget = sharedBudget
			}
			producer, err := (SQLRelightPreintentManualReader{DB: pool, Objects: objects, Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET"), Budget: budget, Limits: RelightManualRasterLimits{PointChecks: 1000000, EdgeEvaluations: 1000000, PayloadBytes: 1000000}}).Load(ctx, who, seed.Tenant, seed.Project, seed.Snapshot, seed.Params, []byte(seed.Source), seed.Kind == "manual_guard")
			if err != nil {
				return RelightPreintentDocuments{}, err
			}
			var external *v2input.Ref
			if len(producer.VectorsBody) > 0 {
				root, err := review.ParseManualRasterDocument(producer.RootBody, review.ArtifactRef{ID: producer.Root.ArtifactID, SHA256: producer.Root.SHA256})
				if err != nil {
					return RelightPreintentDocuments{}, err
				}
				external = &v2input.Ref{ArtifactID: root.Vectors.ID, SHA256: root.Vectors.SHA256}
			}
			expected := 4
			if external != nil {
				expected++
			}
			if len(producer.Inputs) != expected {
				t.Fatal("missing original dependencies")
			}
			return RelightPreintentDocuments{Origin: RelightPreintentProducer{Params: relightProducerLocator{ArtifactID: producer.Original.ArtifactID}}, ParamsBody: producer.ParamsBody, RootBody: producer.RootBody, ExternalVectors: external}, nil
		}
		return reader.Load(ctx, who, seed.Tenant, seed.Project, seed.Snapshot, seed.Params, []byte(seed.Source), seed.Kind)
	}
	assertNoIntent := func() {
		var count int
		if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND v28_relight_intent IS NOT NULL", seed.Tenant).Scan(&count); err != nil || count != 0 {
			t.Fatal("pre-intent reader created intent", err, count)
		}
	}
	assertNoIntent()
	got, err := load()
	if err != nil || got.Origin.Params.ArtifactID != seed.OriginalID || !bytes.Equal(got.ParamsBody, []byte(seed.Original)) || (got.ExternalVectors != nil) != seed.External {
		t.Fatal("original pre-intent document read", err)
	}
	expected := 2
	if seed.Kind == "manual_target" || seed.Kind == "manual_guard" {
		expected = 3
	}
	if seed.Full {
		expected = 5
		if seed.External {
			expected++
		}
	}
	if objects.calls != expected {
		t.Fatal("unexpected original object reads", objects.calls)
	}
	if seed.Full {
		sharedBudget, err = NewRelightManualRasterBudget(RelightManualRasterLimits{PointChecks: 1000000, EdgeEvaluations: 1000000, PayloadBytes: 200})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = load(); err != nil {
			t.Fatal("first shared budget pass", err)
		}
		before := objects.calls
		if value, err := load(); err == nil || value.ParamsBody != nil {
			t.Fatal("exhausted pre-intent raster budget")
		}
		documents := 3
		if seed.External {
			documents++
		}
		if objects.calls-before != documents {
			t.Fatal("exhausted pre-intent budget reached pixels")
		}
		sharedBudget = nil
	}
	// Committed revocation during actual IO must invalidate final discovery.
	objects.afterOpen = func() {
		if _, err := admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", seed.Session); err != nil {
			t.Fatal("session revocation")
		}
	}
	if value, err := load(); err == nil || value.ParamsBody != nil {
		t.Fatal("revoked pre-intent source returned bytes")
	}
	objects.afterOpen = nil
	if _, err = admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", seed.Session); err != nil {
		t.Fatal("session restore")
	}
	if _, err = load(); err != nil {
		t.Fatal("restored source", err)
	}
	before := objects.calls
	if _, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", seed.OriginalID); err != nil {
		t.Fatal("original expiry")
	}
	if value, err := load(); err == nil || value.ParamsBody != nil || objects.calls != before {
		t.Fatal("expired original reached IO")
	}
	assertNoIntent()
}
