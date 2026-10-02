package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type frozenPublishRepo struct{ calls int }

func (r *frozenPublishRepo) Reserve(_ context.Context,
	scope artifactjson.Scope, sha string, _ int64) (
	artifactjson.Reservation, error) {
	r.calls++
	side := strings.TrimPrefix(scope.Purpose, "v23_frozen_")
	ref, err := TrainingFrozenRef(scope.Run, scope.Owner, side, sha)
	return artifactjson.Reservation{ArtifactID: ref.ArtifactID,
		Token: uuid.New()}, err
}

func (r *frozenPublishRepo) Complete(_ context.Context,
	_ artifactjson.Scope, ticket artifactjson.Reservation,
	object objectstore.Object) (artifactjson.Ref, error) {
	return artifactjson.Ref{ID: ticket.ArtifactID,
		SHA256: object.SHA256}, nil
}

type frozenPublishObjects struct{}

func (frozenPublishObjects) PutImmutable(_ context.Context, key string,
	_ io.Reader, size int64, sha, mime string) (objectstore.Object, error) {
	return objectstore.Object{Key: key, Bytes: size, SHA256: sha,
		VersionID: "v1", ContentType: mime}, nil
}

func (frozenPublishObjects) RecoverImmutable(_ context.Context, key string,
	size int64, sha, mime string) (objectstore.Object, error) {
	return objectstore.Object{Key: key, Bytes: size, SHA256: sha,
		VersionID: "v1", ContentType: mime}, nil
}

func TestPublishTrainingFrozenPairRequiresExactManifestRefs(t *testing.T) {
	project, profile, request, manifest := compiledTrainingDraftFixture(t)
	tenant, actor, session := uuid.New(), uuid.New(), uuid.New()
	key := "frozen-key"
	run, err := v2input.RunID(tenant, actor, key)
	if err != nil {
		t.Fatal(err)
	}
	caseRef := &manifest.Examples[0].RenderCase
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	recipe := FrozenRecipe{SchemaVersion: 1, Source: caseRef.Binding,
		BaseStyle: style, Renderer: "v2.2",
		ICCLockSHA:   manifest.ICCLockSHA,
		ModelLockSHA: manifest.ModelLockSHA}
	body, err := canonical.Marshal(recipe)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	compiled := CompiledTrainingRecipes{Base: recipe, Approved: recipe,
		BaseBody: body, ApprovedBody: body, BaseSHA: sha,
		ApprovedSHA: sha}
	base, approved, err := compiled.Refs(run, manifest.Examples[0].EventID)
	if err != nil {
		t.Fatal(err)
	}
	caseRef.BaseRecipe, caseRef.ApprovedRecipe = base, approved
	draft, err := PrepareTrainingDraft(tenant, actor, session, project,
		profile, key, 4096, request, manifest)
	if err != nil {
		t.Fatal(err)
	}
	repo := &frozenPublishRepo{}
	publisher := artifactjson.Publisher{Repository: repo,
		Objects: frozenPublishObjects{}}
	gotBase, gotApproved, err := PublishTrainingFrozenPair(context.Background(),
		publisher, draft, manifest.Examples[0].EventID, compiled)
	if err != nil || gotBase != base || gotApproved != approved || repo.calls != 2 {
		t.Fatalf("frozen publication failed: %v", err)
	}
	compiled.BaseSHA = strings.Repeat("f", 64)
	if _, _, err := PublishTrainingFrozenPair(context.Background(), publisher,
		draft, manifest.Examples[0].EventID, compiled); err == nil || repo.calls != 2 {
		t.Fatal("changed frozen bytes reached publisher")
	}
}
