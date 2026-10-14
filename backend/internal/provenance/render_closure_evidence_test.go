package provenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type signingClosureFunc func(context.Context, auth.Identity, uuid.UUID, []v2input.Entry, v2input.SessionVersionBinding) (v2draft.TaskInputEvidence, error)

func (f signingClosureFunc) ResolveTaskInputEvidence(c context.Context, w auth.Identity, t uuid.UUID, r []v2input.Entry, b v2input.SessionVersionBinding) (v2draft.TaskInputEvidence, error) {
	return f(c, w, t, r, b)
}
func TestSigningRenderClosureRequiresAuthorityAndCancellation(t *testing.T) {
	if _, err := (BoundSigningRenderClosureSource{}).ReadSigningRenderClosure(context.Background(), auth.Identity{}, ReadySigningExport{}, CanonicalSigningTask{}); err == nil {
		t.Fatal("missing closure authority accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (BoundSigningRenderClosureSource{}).ReadSigningRenderClosure(ctx, auth.Identity{}, ReadySigningExport{}, CanonicalSigningTask{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel lost", err)
	}
}

func TestSigningRenderInputDigestBindsEveryTypedEntry(t *testing.T) {
	entries := []v2input.Entry{{Ref: v2input.Ref{ArtifactID: uuid.MustParse("00000000-0000-4000-8000-000000000001"), SHA256: strings.Repeat("a", 64)}, Contract: "WorkingImage"}, {Ref: v2input.Ref{ArtifactID: uuid.MustParse("00000000-0000-4000-8000-000000000002"), SHA256: strings.Repeat("b", 64)}, Contract: "FrozenRenderRecipe"}}
	task := CanonicalSigningTask{Task: uuid.New(), Operation: "render.compose", Version: "1"}
	task.InputDigest, _ = canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{task.Task, task.Operation, task.Version, entries})
	if err := verifySigningRenderInputDigest(task, entries); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"sha", "contract", "missing", "duplicate", "order", "task"} {
		t.Run(mode, func(t *testing.T) {
			changed := append([]v2input.Entry(nil), entries...)
			current := task
			switch mode {
			case "sha":
				changed[0].Ref.SHA256 = strings.Repeat("c", 64)
			case "contract":
				changed[0].Contract = "Artifact"
			case "missing":
				changed = changed[:1]
			case "duplicate":
				changed[1] = changed[0]
			case "order":
				changed[0], changed[1] = changed[1], changed[0]
			case "task":
				current.Task = uuid.New()
			}
			if verifySigningRenderInputDigest(current, changed) == nil {
				t.Fatal("changed inventory accepted")
			}
		})
	}
}
