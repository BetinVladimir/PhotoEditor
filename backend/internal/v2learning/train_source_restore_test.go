package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type restoreFrozen struct{ bodies map[v2input.Ref][]byte }

func (s restoreFrozen) Load(_ context.Context, _ TrainingStart,
	_ TrainingDraftPrepared, _ uuid.UUID, _ string,
	ref v2input.Ref) ([]byte, error) {
	body, okay := s.bodies[ref]
	if !okay {
		return nil, ErrTrainingDraftConflict
	}
	return body, nil
}

func TestTrainingRestoreUsesOnlyPinnedFrozenBytes(t *testing.T) {
	project, profile, request, manifest := compiledTrainingDraftFixture(t)
	start := TrainingStart{TenantID: uuid.New(), ActorID: uuid.New(),
		AuthSessionID: uuid.New(), ProjectID: project, ProfileID: profile,
		Key: "restore-frozen", Request: request}
	runID, _ := v2input.RunID(start.TenantID, start.ActorID, start.Key)
	style := v2input.Ref{ArtifactID: uuid.New(),
		SHA256: strings.Repeat("a", 64)}
	bodies := make(map[v2input.Ref][]byte)
	pairs := make(map[uuid.UUID]CompiledTrainingRecipes)
	for i := range manifest.Examples {
		example := &manifest.Examples[i]
		recipe := FrozenRecipe{SchemaVersion: 1,
			Source: example.RenderCase.Binding, BaseStyle: style,
			Renderer: "v2.2", ICCLockSHA: manifest.ICCLockSHA,
			ModelLockSHA: manifest.ModelLockSHA}
		body, err := canonical.Marshal(recipe)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		sha := hex.EncodeToString(sum[:])
		base, _ := TrainingFrozenRef(runID, example.EventID, "base", sha)
		approved, _ := TrainingFrozenRef(runID, example.EventID, "approved", sha)
		example.RenderCase.BaseRecipe = base
		example.RenderCase.ApprovedRecipe = approved
		bodies[base], bodies[approved] = body, body
		pairs[example.EventID] = CompiledTrainingRecipes{Base: recipe,
			Approved: recipe, BaseBody: body, ApprovedBody: body,
			BaseSHA: sha, ApprovedSHA: sha}
	}
	draft, err := PrepareTrainingDraft(start.TenantID, start.ActorID,
		start.AuthSessionID, project, profile, start.Key, 16*1048576,
		request, manifest)
	if err != nil {
		t.Fatal(err)
	}
	attached, err := AttachTrainingFrozenRecovery(draft, pairs)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := ResumeTrainingDraft(start.TenantID, start.ActorID,
		start.AuthSessionID, project, profile, start.Key, request,
		attached.RecoveryBody)
	if err != nil {
		t.Fatal("frozen recovery rejected", err)
	}
	fromRecovery, err := (TrainingSourceResolver{}).Restore(
		context.Background(), start, recovered)
	if err != nil || len(fromRecovery) != len(pairs) {
		t.Fatalf("unpublished frozen recipes not restored: %d, %v",
			len(fromRecovery), err)
	}
	var tampered trainingDraftRecovery
	if json.Unmarshal(attached.RecoveryBody, &tampered) != nil {
		t.Fatal("invalid recovery fixture")
	}
	tampered.FrozenJCS = strings.Replace(tampered.FrozenJCS,
		strings.Repeat("a", 64), strings.Repeat("b", 64), 1)
	tamperedBody, err := canonical.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ResumeTrainingDraft(start.TenantID, start.ActorID,
		start.AuthSessionID, project, profile, start.Key, request,
		tamperedBody); err == nil {
		t.Fatal("accepted tampered frozen recovery bytes")
	}
	source := TrainingSourceResolver{Frozen: restoreFrozen{bodies}}
	got, err := source.Restore(context.Background(), start, draft)
	if err != nil || len(got) != len(manifest.Examples) {
		t.Fatalf("pending draft not restored: %d, %v", len(got), err)
	}
	first := manifest.Examples[0]
	delete(bodies, first.RenderCase.BaseRecipe)
	if _, err := source.Restore(context.Background(), start, draft); err == nil {
		t.Fatal("restored missing frozen recipe")
	}
}
