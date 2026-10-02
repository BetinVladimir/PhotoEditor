package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRenderAdmissionRejectsMalformedInventoryBeforeDatabase(t *testing.T) {
	for _, name := range []string{"wrong_root_contract", "missing_source", "duplicate", "reverse_order", "wrong_root_sha", "transport_entry", "wrong_digest", "wrong_operation", "wrong_node"} {
		t.Run(name, func(t *testing.T) {
			run := uuid.New()
			workflow, _ := v2input.WorkflowID(run)
			task, _ := v2input.TaskID(workflow, "render.compose", 0)
			root := v2input.Ref{ArtifactID: uuid.MustParse("00000000-0000-4000-8000-000000000001"), SHA256: strings.Repeat("a", 64)}
			source := v2input.Ref{ArtifactID: uuid.MustParse("00000000-0000-4000-8000-000000000002"), SHA256: strings.Repeat("b", 64)}
			entries := []v2input.Entry{{Ref: root, Contract: "FrozenRenderRecipe"}, {Ref: source, Contract: "WorkingImage"}}
			draft := v2input.DraftTask{ID: task, NodeKey: "render.compose:0", Operation: "render.compose", OperationVersion: "1", ResourceClass: "gpu", ParamsArtifactID: root.ArtifactID, ParamsSHA: root.SHA256}
			switch name {
			case "wrong_root_contract":
				entries[0].Contract = "FrozenRecipe"
			case "missing_source":
				entries[1].Contract = "StyleV2"
			case "duplicate":
				entries[1] = entries[0]
			case "reverse_order":
				entries[0], entries[1] = entries[1], entries[0]
			case "wrong_root_sha":
				entries[0].Ref.SHA256 = strings.Repeat("c", 64)
			case "transport_entry":
				entries[1].Contract = "InputClosureManifest"
			case "wrong_operation":
				draft.Operation = "layer.compose"
			case "wrong_node":
				draft.NodeKey = "render.compose:1"
			}
			digest, err := canonical.Hash(struct {
				Task      uuid.UUID       `json:"task"`
				Operation string          `json:"operation"`
				Version   string          `json:"version"`
				Entries   []v2input.Entry `json:"entries"`
			}{task, "render.compose", "1", entries})
			if err != nil {
				t.Fatal(err)
			}
			draft.InputDigest = digest
			if name == "wrong_digest" {
				draft.InputDigest = strings.Repeat("d", 64)
			}
			graph := v2input.GraphTask{ID: task, Built: v2input.Built{Count: len(entries)}, Direct: []v2input.Ref{root}, Open: func(context.Context) (v2input.Cursor, error) { return &compositionCursor{entries: entries}, nil }}
			// An uninitialized pool cannot begin a transaction: rejection must happen
			// before touching it, including inventories with a freshly matching digest.
			err = (RenderInputAdmission{DB: &pgxpool.Pool{}}).RegisterRenderInputs(context.Background(), uuid.New(), run, workflow, 0, draft, graph)
			if !errors.Is(err, v2input.ErrInvalid) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
