package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type createRecheckCatalog func(context.Context, auth.Identity, uuid.UUID, v2draft.LayerRef) (objectstore.Object, error)

func (f createRecheckCatalog) Lookup(ctx context.Context, who auth.Identity, tid uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	return f(ctx, who, tid, node)
}

func TestCreateClosureRechecksRepeatedOccurrencesAndPhysicalVersion(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tid := uuid.New()
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("leaf"))}
	object := objectstore.Object{Key: "private/key", VersionID: "v1", SHA256: ref.SHA256, ContentType: "application/octet-stream", Bytes: 4}
	first := v2draft.LayerRef{Ref: ref, Contract: "WorkingPixels", Path: "source.pixels"}
	second := first
	second.Path = "frame.source.pixels"
	occurrences := []v2draft.OccurrenceWitness{{Node: first, Object: object}, {Node: second, Object: object}}
	for _, mode := range []string{"success", "changed", "denied", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			catalog := createRecheckCatalog(func(_ context.Context, actor auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
				if actor != who || tenant != tid || node.Path != occurrences[calls].Node.Path {
					t.Fatal("occurrence scope changed")
				}
				calls++
				if calls == 2 {
					switch mode {
					case "changed":
						changed := object
						changed.VersionID = "v2"
						return changed, nil
					case "denied":
						return objectstore.Object{}, errors.New("revoked")
					case "cancel":
						cancel()
					}
				}
				return object, nil
			})
			err := recheckCreateRecipeOccurrences(ctx, who, tid, catalog, occurrences)
			if (err == nil) != (mode == "success") || calls != 2 {
				t.Fatalf("mode=%s calls=%d error=%v", mode, calls, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := recheckCreateRecipeOccurrences(ctx, who, tid, nil, occurrences); err != context.Canceled {
		t.Fatalf("cancel preflight: %v", err)
	}
	if err := recheckCreateRecipeOccurrences(context.Background(), who, tid, nil, occurrences); err == nil {
		t.Fatal("missing catalog")
	}
}
