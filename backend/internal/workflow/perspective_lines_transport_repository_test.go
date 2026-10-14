package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPerspectiveLinesTransportRepositoryOwnsBodyAndRefusesWrongScope(t *testing.T) {
	scope, source, icc := perspectiveLinesFixture(t, false)
	plan, err := CompilePerspectiveLinesPlan(scope, source, icc)
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
	entries, err := plan.Entries(params)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(v2input.Chunk{SchemaVersion: 1, Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := CompilePerspectiveLinesTransportDocument(plan, params, 0, "InputClosureChunk", raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := newPerspectiveLinesTransportRepository(&pgxpool.Pool{}, "photos", doc)
	if err != nil {
		t.Fatal(err)
	}
	original := repository.body
	doc.body[0] = 'x'
	if repository.body != original {
		t.Fatal("document bytes mutable")
	}
	for _, alter := range []func(*artifactjson.Scope){
		func(s *artifactjson.Scope) { s.Tenant = uuid.New() },
		func(s *artifactjson.Scope) { s.Owner = uuid.New() },
		func(s *artifactjson.Scope) { s.Run = uuid.New() },
		func(s *artifactjson.Scope) { s.Purpose = "v2_input_manifest" },
		func(s *artifactjson.Scope) { s.AssemblyLease = uuid.New() },
	} {
		foreign := repository.scope
		alter(&foreign)
		if _, err := repository.Reserve(context.Background(), foreign, repository.sha, int64(len(original))); err == nil {
			t.Fatal("foreign reserve accepted")
		}
		if _, err := repository.Complete(context.Background(), foreign, artifactjson.Reservation{}, objectstore.Object{}); err == nil {
			t.Fatal("foreign completion accepted")
		}
	}
	if _, err := repository.Reserve(context.Background(), repository.scope, repository.sha, int64(len(original))+1); err == nil {
		t.Fatal("wrong size accepted")
	}
}
func TestPerspectiveLinesSQLClosurePublicationDependencyAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublishPerspectiveLinesClosure(ctx, nil, nil, "photos", PerspectiveLinesPlan{}, review.ArtifactRef{}); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := PublishPerspectiveLinesClosure(context.Background(), nil, nil, "photos", PerspectiveLinesPlan{}, review.ArtifactRef{}); err == nil {
		t.Fatal("missing publisher dependencies accepted")
	}
	if _, err := newPerspectiveLinesTransportRepository(&pgxpool.Pool{}, "photos", PerspectiveLinesTransportDocument{}); err == nil {
		t.Fatal("empty proof accepted")
	}
	docs := &perspectiveLinesTransportDocuments{}
	if _, err := docs.Publish(context.Background(), artifactjson.Scope{Tenant: uuid.New()}, []byte(`{}`)); err == nil {
		t.Fatal("foreign scope accepted")
	}
}
