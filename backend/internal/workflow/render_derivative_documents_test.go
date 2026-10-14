package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2render"
)

func TestDerivativePublisherOwnsEvidenceAndRejectsScopeSubstitution(t *testing.T) {
	// An uninitialized pool makes any accidental database access fail this test.
	pool := &pgxpool.Pool{}
	run := uuid.New()
	params, root, image := []byte(`{"a":1}`), []byte(`{"r":1}`), []byte(`{"i":1}`)
	doc := RenderDerivativeDocument{Run: run, ID: uuid.NewSHA1(run, []byte("render.derivative:preview1600")), Variant: "preview1600", Params: params, ParamsSHA: renderDerivativeBytesSHA(params)}
	r, err := NewRenderDerivativeDocuments(pool, uuid.New(), "photos", doc, root, image)
	if err != nil {
		t.Fatal(err)
	}
	params[2], root[2], image[2] = 'z', 'z', 'z'
	if r.params != `{"a":1}` || r.root != `{"r":1}` || r.image != `{"i":1}` {
		t.Fatal("publisher borrowed mutable evidence")
	}
	wrong := r.scope
	wrong.Tenant = uuid.New()
	if _, err = r.Reserve(context.Background(), wrong, r.sha, int64(len(r.params))); !errors.Is(err, objectstore.ErrInvalid) {
		t.Fatal("tenant substitution", err)
	}
	if _, err = r.Reserve(context.Background(), r.scope, r.sha, int64(len(r.params)+1)); !errors.Is(err, objectstore.ErrInvalid) {
		t.Fatal("size substitution", err)
	}
	v := artifactjson.Reservation{ArtifactID: uuid.New(), Token: uuid.New()}
	key, err := artifactjson.DocumentKey(r.scope, v.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	o := objectstore.Object{Key: key, SHA256: r.sha, Bytes: int64(len(r.params)), ContentType: "application/json", VersionID: "null"}
	if _, err = r.Complete(context.Background(), r.scope, v, o); !errors.Is(err, objectstore.ErrIntegrity) {
		t.Fatal("unversioned object accepted", err)
	}
	o.VersionID, o.Key = "immutable-version", key+"-replaced"
	if _, err = r.Complete(context.Background(), r.scope, v, o); !errors.Is(err, objectstore.ErrIntegrity) {
		t.Fatal("key substitution", err)
	}
}

func TestDerivativePublicationCancellationPrecedesDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tasks, err := PublishRenderDerivativeTasks(ctx, nil, nil, "", v2render.RenderDerivativeIntent{}, nil, nil)
	if !errors.Is(err, context.Canceled) || tasks != nil {
		t.Fatal("cancelled publication did not stop", err)
	}
}
