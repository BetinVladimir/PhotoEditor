package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

func TestPerspectiveLinesDocumentsOwnParamsAndRejectForeignScope(t *testing.T) {
	scope, raw, icc := perspectiveLinesFixture(t, false)
	plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewPerspectiveLinesDocuments(&pgxpool.Pool{}, "photos", plan)
	if err != nil {
		t.Fatal(err)
	}
	original := repository.params
	plan.params[0] = 'x'
	if repository.params != original {
		t.Fatal("mutable Params escaped")
	}
	for _, mutate := range []func(*artifactjson.Scope){
		func(s *artifactjson.Scope) { s.Tenant = uuid.New() },
		func(s *artifactjson.Scope) { s.Run = uuid.New() },
		func(s *artifactjson.Scope) { s.Owner = uuid.New() },
		func(s *artifactjson.Scope) { s.Purpose = "v2_input_manifest" },
	} {
		foreign := repository.scope
		mutate(&foreign)
		if _, err := repository.Reserve(context.Background(), foreign, repository.sha, int64(len(original))); err == nil {
			t.Fatal("foreign reserve accepted")
		}
		if _, err := repository.Complete(context.Background(), foreign, artifactjson.Reservation{ArtifactID: uuid.New(), Token: uuid.New()}, objectstore.Object{}); err == nil {
			t.Fatal("foreign completion accepted")
		}
	}
	if _, err := repository.Reserve(context.Background(), repository.scope, repository.sha, int64(len(original))+1); err == nil {
		t.Fatal("wrong size accepted")
	}
}
func TestPerspectiveLinesDocumentsRejectCompletionSubstitutionBeforeDatabase(t *testing.T) {
	scope, raw, icc := perspectiveLinesFixture(t, true)
	plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewPerspectiveLinesDocuments(&pgxpool.Pool{}, "photos", plan)
	if err != nil {
		t.Fatal(err)
	}
	ticket := artifactjson.Reservation{ArtifactID: uuid.New(), Token: uuid.New()}
	key, err := artifactjson.DocumentKey(repository.scope, ticket.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	original := objectstore.Object{Key: key, SHA256: repository.sha, Bytes: int64(len(repository.params)), ContentType: "application/json", VersionID: "version-1"}
	for _, mutate := range []func(*objectstore.Object){
		func(o *objectstore.Object) { o.Key += "-foreign" },
		func(o *objectstore.Object) { o.SHA256 = "bad" },
		func(o *objectstore.Object) { o.Bytes++ },
		func(o *objectstore.Object) { o.ContentType = "text/plain" },
		func(o *objectstore.Object) { o.VersionID = "null" },
	} {
		object := original
		mutate(&object)
		if _, err := repository.Complete(context.Background(), repository.scope, ticket, object); err == nil {
			t.Fatal("substitution accepted")
		}
	}
}
func TestPerspectiveLinesPublicationRejectsMissingDependencyAndCancelledContext(t *testing.T) {
	scope, raw, icc := perspectiveLinesFixture(t, false)
	plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPerspectiveLinesDocuments(nil, "photos", plan); err == nil {
		t.Fatal("missing DB accepted")
	}
	if _, err := PublishPerspectiveLinesTask(context.Background(), nil, nil, "photos", plan); err == nil {
		t.Fatal("missing objects accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublishPerspectiveLinesTask(ctx, nil, nil, "photos", plan); err != context.Canceled {
		t.Fatal(err)
	}
}
