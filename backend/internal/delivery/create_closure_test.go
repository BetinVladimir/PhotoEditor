package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type createRecipeResolverFunc func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, v2input.SessionSnapshotItem) ([]v2input.Entry, error)

func (f createRecipeResolverFunc) ResolveCreateRecipeInputs(ctx context.Context, who auth.Identity, tid, project uuid.UUID, item v2input.SessionSnapshotItem) ([]v2input.Entry, error) {
	return f(ctx, who, tid, project, item)
}

func TestCreateClosureResolvesScopedDependenciesAndPinsInventory(t *testing.T) {
	i, session, catalog, items := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	raw, err := i.EncodePending(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	leaf := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("pixels"))}, Contract: "WorkingPixels"}
	entries := []v2input.Entry{{Ref: items[0].WorkingImage, Contract: "WorkingImage"}, {Ref: items[0].Recipe, Contract: "FrozenRenderRecipe"}, leaf, leaf}
	calls := 0
	resolver := createRecipeResolverFunc(func(_ context.Context, actor auth.Identity, tid, project uuid.UUID, item v2input.SessionSnapshotItem) ([]v2input.Entry, error) {
		calls++
		if actor != who || tid != i.tenant || project != i.request.ProjectID || item.Binding != items[0].Binding {
			t.Fatal("resolver scope changed")
		}
		return entries, nil
	})
	limits := PrepareResourceLimits{RAMBytes: 8 << 30, ScratchBytes: 8 << 30, OutputBytes: 8 << 20}
	frozen, err := i.ResolvePrepareTask(context.Background(), who, raw, resolver, limits)
	if err != nil || calls != 1 {
		t.Fatalf("resolve: %v calls=%d", err, calls)
	}
	docs, err := i.CompileDocuments(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := docs.Plan.FreezeTask(append(docs.Plan.Roots(), leaf), limits)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	other, err := expected.DraftTask()
	if err != nil || draft.InputDigest != other.InputDigest {
		t.Fatal("transitive leaf missing or duplicate charged")
	}
	entries[2].Ref.SHA256 = digest([]byte("changed"))
	again, err := frozen.DraftTask()
	if err != nil || again.InputDigest != draft.InputDigest {
		t.Fatal("borrowed resolver inventory")
	}
}

func TestCreateClosureRefusesMissingRootsCollisionsAndResolverFailure(t *testing.T) {
	i, session, catalog, items := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	raw, err := i.EncodePending(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	limits := PrepareResourceLimits{RAMBytes: 8 << 30, ScratchBytes: 8 << 30, OutputBytes: 8 << 20}
	for _, kind := range []string{"missing", "collision", "params", "error", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			resolver := createRecipeResolverFunc(func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, v2input.SessionSnapshotItem) ([]v2input.Entry, error) {
				entries := []v2input.Entry{{Ref: items[0].WorkingImage, Contract: "WorkingImage"}, {Ref: items[0].Recipe, Contract: "FrozenRenderRecipe"}}
				switch kind {
				case "missing":
					entries[1].Ref.ArtifactID = uuid.New()
				case "collision":
					changed := entries[0]
					changed.Ref.SHA256 = digest([]byte("foreign"))
					entries = append(entries, changed)
				case "params":
					entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("params"))}, Contract: "Params"})
				case "error":
					return nil, errors.New("catalog unavailable")
				case "cancel":
					cancel()
				}
				return entries, nil
			})
			if _, err := i.ResolvePrepareTask(ctx, who, raw, resolver, limits); err == nil {
				t.Fatal("invalid closure accepted")
			}
		})
	}
}
