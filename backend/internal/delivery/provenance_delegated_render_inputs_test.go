package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

func TestDelegatedRenderInputCatalogRequiresScope(t *testing.T) {
	catalog := DelegatedRenderInputCatalog{}
	if _, err := catalog.ReadDelegatedRenderInputs(context.Background(), provenance.CanonicalSigningTask{}, v2input.Ref{}); err == nil {
		t.Fatal("unconfigured render input catalog accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.ReadDelegatedRenderInputs(ctx, provenance.CanonicalSigningTask{}, v2input.Ref{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}

func TestDelegatedRenderInputInventoryBindsBothRootsAndDigest(t *testing.T) {
	params := v2input.Ref{ArtifactID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), SHA256: strings.Repeat("a", 64)}
	source := v2input.Ref{ArtifactID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), SHA256: strings.Repeat("b", 64)}
	entries := []v2input.Entry{{Ref: params, Contract: "FrozenRenderRecipe"}, {Ref: source, Contract: "WorkingImage"}}
	task := provenance.CanonicalSigningTask{Task: uuid.New(), Operation: "render.compose", Version: "1", Params: params}
	digest, err := canonical.Hash(struct {
		Task uuid.UUID `json:"task"`
		Operation string `json:"operation"`
		Version string `json:"version"`
		Entries []v2input.Entry `json:"entries"`
	}{task.Task, task.Operation, task.Version, entries})
	if err != nil { t.Fatal(err) }
	task.InputDigest = digest
	if err := verifyDelegatedRenderInputInventory(task, source, entries); err != nil { t.Fatal(err) }
	for name, changed := range map[string][]v2input.Entry{
		"missing source": {{Ref: params, Contract: "FrozenRenderRecipe"}, {Ref: source, Contract: "Artifact"}},
		"wrong recipe contract": {{Ref: params, Contract: "Params"}, {Ref: source, Contract: "WorkingImage"}},
		"unsorted": {entries[1], entries[0]},
	} {
		if err := verifyDelegatedRenderInputInventory(task, source, changed); err == nil { t.Fatal(name) }
	}
	wrong := task
	wrong.InputDigest = strings.Repeat("f", 64)
	if err := verifyDelegatedRenderInputInventory(wrong, source, entries); err == nil { t.Fatal("changed digest accepted") }
}
