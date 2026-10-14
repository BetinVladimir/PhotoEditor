package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/objectstore"
	"testing"
	"time"
)

type relightObservedObjects struct {
	store     *objectstore.Store
	calls     int
	afterOpen func()
}

func (o *relightObservedObjects) Open(ctx context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	o.calls++
	stream, err := o.store.Open(ctx, obj)
	if err == nil && o.afterOpen != nil {
		o.afterOpen()
	}
	return stream, err
}

func TestIntegrationRelightDepthReader(t *testing.T) {
	path := os.Getenv("PHOTO_TEST_RELIGHT_DEPTH_SEED")
	if path == "" {
		t.Skip("isolated PostgreSQL/MinIO seed required")
	}
	var seed struct {
		Producer   string        `json:"producer"`
		Subject    string        `json:"subject"`
		Protected  string        `json:"protected"`
		Tenant     uuid.UUID     `json:"tenant"`
		Run        uuid.UUID     `json:"run"`
		Intent     RelightIntent `json:"intent"`
		Source     string        `json:"source"`
		Depth      string        `json:"depth"`
		Original   string        `json:"original"`
		OriginalID uuid.UUID     `json:"original_id"`
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &seed) != nil {
		t.Fatal("invalid isolated seed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("invalid isolated database configuration")
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE photo_scheduler")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("isolated scheduler pool unavailable")
	}
	defer pool.Close()
	admin, err := pgx.Connect(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("isolated control connection unavailable")
	}
	defer admin.Close(context.Background())
	store, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET"), AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal("isolated object store unavailable")
	}
	objects := &relightObservedObjects{store: store}
	reader := SQLRelightDepthReader{DB: pool, Objects: objects, Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET")}
	var sharedManualBudget *RelightManualRasterBudget
	load := func() (RelightDepthProducer, error) {
		if seed.Producer == "manual" {
			budget, err := NewRelightManualRasterBudget(RelightManualRasterLimits{PointChecks: 1000000, EdgeEvaluations: 1000000, PayloadBytes: 1000000})
			if err != nil {
				return RelightDepthProducer{}, err
			}
			if sharedManualBudget != nil {
				budget = sharedManualBudget
			}
			producer, err := (SQLRelightManualReader{Budget: budget, DB: pool, Objects: objects, Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET"), Limits: RelightManualRasterLimits{PointChecks: 1000000, EdgeEvaluations: 1000000, PayloadBytes: 1000000}}).Load(ctx, seed.Tenant, seed.Run, seed.Intent, RelightInputDocuments{Source: []byte(seed.Source), Subject: []byte(seed.Subject), Protected: []byte(seed.Protected)}, true)
			return RelightDepthProducer{Ref: producer.Original, Body: producer.ParamsBody}, err
		}
		if seed.Producer == "person" {
			return (SQLRelightPersonReader{DB: pool, Objects: objects, Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET")}).Load(ctx, seed.Tenant, seed.Run, seed.Intent, RelightInputDocuments{Source: []byte(seed.Source), Subject: []byte(seed.Subject), Protected: []byte(seed.Protected)})
		}
		return reader.Load(ctx, seed.Tenant, seed.Run, seed.Intent, []byte(seed.Source), []byte(seed.Depth))
	}
	result, err := load()
	if err != nil || result.Ref.ArtifactID != seed.OriginalID || !bytes.Equal(result.Body, []byte(seed.Original)) || objects.calls < 1 {
		t.Fatal("actual pinned original depth Params read failed", err)
	}

	if seed.Producer == "manual" {
		sharedManualBudget, err = NewRelightManualRasterBudget(RelightManualRasterLimits{PointChecks: 1000000, EdgeEvaluations: 1000000, PayloadBytes: 200})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = load(); err != nil {
			t.Fatal("first shared-budget pass", err)
		}
		before := objects.calls
		if got, err := load(); err == nil || got.Body != nil {
			t.Fatal("shared request exceeded aggregate budget")
		}
		// Root/Params and optional external vectors may be read to derive work;
		// exhausted budget must prevent both pixel object reads.
		documents := 2
		var original map[string]json.RawMessage
		if json.Unmarshal([]byte(seed.Original), &original) != nil {
			t.Fatal("original Params")
		}
		if string(original["vectors"]) != "null" {
			documents++
		}
		if objects.calls-before != documents {
			t.Fatal("exhausted budget reached pixel IO")
		}
		sharedManualBudget = nil
	}
	// A committed policy revocation after actual object open must invalidate the
	// second scheduler discovery, even though the bytes are valid and unchanged.
	beforeRevocation := objects.calls
	objects.afterOpen = func() {
		if _, err := admin.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", seed.Intent.Qualification.PolicyID); err != nil {
			t.Fatal("isolated revocation failed")
		}
	}
	if got, err := load(); err == nil || got.Body != nil || objects.calls != beforeRevocation+1 {
		t.Fatal("revocation during real object read accepted")
	}
	objects.afterOpen = nil
	if _, err := admin.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", seed.Intent.Qualification.PolicyID); err != nil {
		t.Fatal("isolated policy restore failed")
	}
	if _, err = load(); err != nil {
		t.Fatal("restored origin failed", err)
	}
	before := objects.calls
	if _, err := admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", seed.OriginalID); err != nil {
		t.Fatal("isolated original expiry failed")
	}
	if got, err := load(); err == nil || got.Body != nil || objects.calls != before {
		t.Fatal("expired original reached object IO")
	}
}
