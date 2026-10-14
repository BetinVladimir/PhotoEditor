package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestManualPreparationProofCannotCrossScopes(t *testing.T) {
	tenantID, project := uuid.New(), uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
	proof := manualPreparationProof{tenant: tenantID, project: project, actor: who.UserID, session: who.SessionID, binding: binding}
	proof.objects.Root.ref.ArtifactID = uuid.New()
	if !validManualPreparationScope(project, who, tenantID, binding, proof) {
		t.Fatal("original scope refused")
	}
	mutations := []func(*manualPreparationProof){
		func(p *manualPreparationProof) { p.tenant = uuid.New() },
		func(p *manualPreparationProof) { p.project = uuid.New() },
		func(p *manualPreparationProof) { p.actor = uuid.New() },
		func(p *manualPreparationProof) { p.session = uuid.New() },
		func(p *manualPreparationProof) { p.binding.AssetVersionID = uuid.New() },
		func(p *manualPreparationProof) { p.binding.Source.ArtifactID = uuid.New() },
		func(p *manualPreparationProof) { p.binding.Source.SHA256 = strings.Repeat("c", 64) },
		func(p *manualPreparationProof) { p.binding.CanvasHash = strings.Repeat("d", 64) },
		func(p *manualPreparationProof) { p.objects.Root.ref.ArtifactID = uuid.Nil },
	}
	for _, mutate := range mutations {
		changed := proof
		mutate(&changed)
		if validManualPreparationScope(project, who, tenantID, binding, changed) {
			t.Fatal("cross-scope proof accepted")
		}
		reader := SQLTransferSourceReader{ProjectID: project}
		if err := reader.recheckManualPreparationProof(context.Background(), who, tenantID, binding, changed); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("scope refusal reached catalog", err)
		}
	}
	// Matching malformed identities must not become a valid proof scope.
	invalid := binding
	invalid.CanvasHash = "invalid"
	proof.binding = invalid
	if validManualPreparationScope(project, who, tenantID, invalid, proof) {
		t.Fatal("malformed matching canvas accepted")
	}
	proof.binding = binding
	if validManualPreparationScope(project, auth.Identity{}, tenantID, binding, proof) {
		t.Fatal("empty principal accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (SQLTransferSourceReader{ProjectID: project}).recheckManualPreparationProof(ctx, who, tenantID, binding, proof); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled proof reached catalog")
	}
}
