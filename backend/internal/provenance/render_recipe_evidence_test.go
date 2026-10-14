package provenance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestSigningRenderRecipeBindsExactFrozenBytesAndCurrentScope(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	authored, err := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": v2draft.CorrectionVector{EV: .1}})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := v2draft.BuildFrozenRenderRecipe(authored, v2draft.ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("d", 64), ModelLockSHA: strings.Repeat("e", 64)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "operation", "version", "session", "hash", "invalid_recipe", "changed", "revoked", "cancel_before", "cancel_after", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			body := append([]byte(nil), frozen...)
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New(), Task: uuid.New(), Run: uuid.New()}
			task := CanonicalSigningTask{Task: ready.Task, Run: ready.Run, Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "render.compose", Version: "1", InputDigest: strings.Repeat("f", 64), ResultDigest: strings.Repeat("a", 64), Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(body)}, Inputs: json.RawMessage(`[]`), Outputs: json.RawMessage(`[]`), Metrics: json.RawMessage(`{}`)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "operation":
				task.Operation = "export.encode"
			case "version":
				task.Version = "2"
			case "session":
				who.SessionID = uuid.Nil
			case "hash":
				task.Params.SHA256 = strings.Repeat("0", 64)
			case "invalid_recipe":
				body = []byte(`{}`)
				task.Params.SHA256 = bodyDigest(body)
			case "cancel_before":
				cancel()
			case "oversized":
				body = make([]byte, (1<<20)+1)
				task.Params.SHA256 = bodyDigest(body)
			}
			calls := 0
			denied := errors.New("access revoked")
			reader := signingDocumentsFunc(func(_ context.Context, w auth.Identity, r ReadySigningExport, e CanonicalSigningTask, ref v2input.Ref, maximum int) ([]byte, error) {
				calls++
				if w != who || r != ready || e.Task != task.Task || ref != task.Params || maximum != 1<<20 {
					t.Fatal("reader scope changed")
				}
				if calls == 2 {
					switch mode {
					case "changed":
						return append(append([]byte(nil), body...), ' '), nil
					case "revoked":
						return nil, denied
					case "cancel_after":
						cancel()
					}
				}
				return body, nil
			})
			result, err := ReadSigningRenderRecipe(ctx, reader, who, ready, task)
			if mode == "valid" {
				if err != nil || calls != 2 || result.Scalars.Source != binding {
					t.Fatal("valid recipe refused", err)
				}
				closureEntries := []v2input.Entry{{Ref: binding.Source, Contract: "WorkingImage"}, {Ref: task.Params, Contract: "FrozenRenderRecipe"}}
				sort.Slice(closureEntries, func(i, j int) bool {
					return bytes.Compare(closureEntries[i].Ref.ArtifactID[:], closureEntries[j].Ref.ArtifactID[:]) < 0
				})
				closureTask := task
				closureTask.InputDigest, _ = canonical.Hash(struct {
					Task      uuid.UUID       `json:"task"`
					Operation string          `json:"operation"`
					Version   string          `json:"version"`
					Entries   []v2input.Entry `json:"entries"`
				}{task.Task, task.Operation, task.Version, closureEntries})
				chunkBody, _ := canonical.Marshal(v2input.Chunk{SchemaVersion: 1, Entries: closureEntries})
				chunkRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(chunkBody)}
				entriesSHA, _ := canonical.Hash(closureEntries)
				manifestBody, _ := canonical.Marshal(v2input.Manifest{SchemaVersion: 1, TaskID: task.Task, EntryCount: len(closureEntries), EntriesSHA: entriesSHA, Chunks: []v2input.Ref{chunkRef}})
				manifestRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(manifestBody)}
				closureTask.Inputs, _ = canonical.Marshal([]v2input.Ref{manifestRef, task.Params})
				closureDocuments := signingDocumentsFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, _ CanonicalSigningTask, r v2input.Ref, _ int) ([]byte, error) {
					switch r {
					case task.Params:
						return body, nil
					case chunkRef:
						return chunkBody, nil
					case manifestRef:
						return manifestBody, nil
					default:
						return nil, ErrInvalid
					}
				})
				for _, change := range []bool{false, true} {
					closureCalls := 0
					closure := signingClosureFunc(func(_ context.Context, _ auth.Identity, tenant uuid.UUID, roots []v2input.Entry, b v2input.SessionVersionBinding) (v2draft.TaskInputEvidence, error) {
						closureCalls++
						if tenant != ready.Tenant || b != binding || len(roots) != 2 || roots[0].Ref != binding.Source || roots[1].Ref != task.Params {
							t.Fatal("closure scope changed")
						}
						document := string(body)
						if change && closureCalls == 2 {
							document = "{}"
						}
						return v2draft.TaskInputEvidence{Entries: closureEntries, Documents: []v2draft.DocumentWitness{{Ref: task.Params, Contract: "FrozenRenderRecipe", JCS: document}}, Occurrences: []v2draft.OccurrenceWitness{}}, nil
					})
					proof, closureErr := (BoundSigningRenderClosureSource{Documents: closureDocuments, Closure: closure}).ReadSigningRenderClosure(ctx, who, ready, closureTask)
					if change {
						if closureErr == nil {
							t.Fatal("changed closure accepted")
						}
					} else if closureErr != nil || closureCalls != 2 || len(proof.inventory.Entries) != 2 {
						t.Fatal("valid closure refused", closureErr)
					}
				}
				body[0] = 'x'
				if result.Recipe[0] != '{' {
					t.Fatal("returned recipe aliases reader buffer")
				}
				return
			}
			if err == nil {
				t.Fatal("invalid recipe evidence accepted")
			}
			if mode == "revoked" && !errors.Is(err, denied) {
				t.Fatal("access error lost", err)
			}
			if (mode == "cancel_before" || mode == "cancel_after") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancel error lost", err)
			}
			if (mode == "operation" || mode == "version" || mode == "session" || mode == "cancel_before") && calls != 0 {
				t.Fatal("invalid scope read documents")
			}
		})
	}
}
