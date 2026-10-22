package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

// This fixture exercises the actual transport compiler, not domain admission.
// Operation-specific roots are synthetic pinned references; no model/quality
// qualification or source authority is inferred from this pure SQL packet.
type manifestSQLPublisher struct{}

func (manifestSQLPublisher) Publish(_ context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	name := "v2_input_manifest"
	if contract == "InputClosureChunk" {
		name = "v2_input_chunk_0"
	}
	sum := sha256.Sum256(body)
	return v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte(name)), SHA256: hex.EncodeToString(sum[:])}, nil
}
func TestWriteSemanticTransportManifestSQLFixtures(t *testing.T) {
	path := os.Getenv("PHOTO_TEST_MANIFEST_FIXTURES")
	if path == "" {
		t.Skip("SQL fixture export not requested")
	}
	operations := []string{"semantic.preview_local_exposure", "semantic.adopt_shown_exposure", "semantic.preview_remove_object", "semantic.adopt_shown_removal", "semantic.preview_background_cleanup", "semantic.adopt_shown_background_cleanup", "semantic.preview_retouch_region", "semantic.adopt_shown_retouch_region"}
	var rows []map[string]any
	for index, op := range operations {
		parent, target := uuid.New(), uuid.New()
		key := op + ":" + target.String()
		task := uuid.NewSHA1(parent, []byte(key))
		params := map[string]any{"target_id": target, "child_run_id": parent}
		profile := map[string]any{}
		entries := []v2input.Entry{}
		add := func(contract string) v2input.Ref {
			sum := sha256.Sum256([]byte(contract))
			ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
			entries = append(entries, v2input.Entry{Ref: ref, Contract: contract})
			return ref
		}
		if index%2 == 0 {
			params["plan"] = add("SemanticPlan")
			params["intent"] = add("EditIntent")
			params["candidate"] = add("TransferCandidate")
			params["source"] = map[string]any{"source": add("WorkingImage")}
			params["protection"] = add("MaskRef")
			if index == 6 {
				profile["retouch_context"] = add("RetouchPreviewAdapterContext")
			}
		} else {
			family := []string{"Exposure", "Removal", "BackgroundCleanup", "Retouch"}[index/2]
			params["approved_edit"] = add("ApprovedSemanticEdit")
			params["presentation"] = add(family + "PreviewPresentation")
			params["numerical_evidence"] = add(family + "PreviewNumericalEvidence")
			params["working_image"] = add("WorkingImage")
			params["pixels"] = add("NpyArray")
		}
		body, err := canonical.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		ref := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("Params")), SHA256: hex.EncodeToString(sum[:])}
		entries = append(entries, v2input.Entry{Ref: ref, Contract: "Params"})
		closure, err := v2input.FreezeInputInventory(task, op, body, ref, entries)
		if err != nil {
			t.Fatal(err)
		}
		cursor, err := closure.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		built, err := v2input.BuildInputClosure(context.Background(), task, cursor, manifestSQLPublisher{})
		if err != nil {
			t.Fatal(err)
		}
		cursor, err = closure.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ordered := []v2input.Entry{}
		for {
			entry, more, err := cursor.Next(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !more {
				break
			}
			ordered = append(ordered, entry)
		}
		resourceClass := "cpu"
		if index == 2 || index == 4 {
			resourceClass = "gpu"
		}
		node := map[string]any{"id": task, "node_key": key, "operation": op, "operation_version": "1", "resource_class": resourceClass, "params_artifact_id": ref.ArtifactID, "params_sha": ref.SHA256, "input_digest": closure.InputDigest, "execution_profile": profile}
		rows = append(rows, map[string]any{"parent": parent, "params": params, "node": node, "inventory": map[string]any{"task_id": task, "entries": ordered}, "transport": map[string]any{"task_id": task, "entry_count": built.Count, "entries_sha": built.SHA256, "manifest": built.Manifest, "chunks": built.Chunks, "direct_roots": closure.Direct}})
	}
	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
