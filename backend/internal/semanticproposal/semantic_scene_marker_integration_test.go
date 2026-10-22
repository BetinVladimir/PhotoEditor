package semanticproposal

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Pure installed SQL identity parity, not qualification/admission acceptance.
// Authored only; no database or migration has been started for this change.
func TestIntegrationSceneMarkerIdentityMatchesGoNamespace(t *testing.T) {
	url := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("installed owned test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_fn_owner"
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sha := strings.Repeat("a", 64)
	parent := uuid.New()
	source := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, CanvasHash: sha}
	bindingSHA, err := canonical.Hash(source)
	if err != nil {
		t.Fatal(err)
	}
	namespace := uuid.NewSHA1(parent, []byte("semantic.scene.source:"+bindingSHA))
	for _, backend := range []string{"cuda", "mps"} {
		for _, operation := range []string{"scene.parse", "depth.predict"} {
			run := uuid.NewSHA1(namespace, []byte("semantic.scene."+operation+".run"))
			workflow, err := v2input.WorkflowID(run)
			if err != nil {
				t.Fatal(err)
			}
			task, err := v2input.TaskID(workflow, operation, 0)
			if err != nil {
				t.Fatal(err)
			}
			frozen := v2input.DraftTask{ID: task, NodeKey: operation + ":0", Operation: operation, OperationVersion: "1", ResourceClass: "gpu", ParamsArtifactID: uuid.New(), ParamsSHA: sha, InputDigest: sha, ExecutionProfile: json.RawMessage(`{"backend":"` + backend + `"}`)}
			taskSHA, err := canonical.Hash(frozen)
			if err != nil {
				t.Fatal(err)
			}
			marker := map[string]any{"schema_version": 1, "operation": operation, "parent_run_id": parent, "request_sha": sha, "auth_session_id": uuid.New(), "source": source, "source_binding_sha": bindingSHA, "source_document_sha": sha, "params_document": uuid.NewSHA1(namespace, []byte("semantic.scene."+operation+".params")), "params": v2input.Ref{ArtifactID: frozen.ParamsArtifactID, SHA256: sha}, "task_id": task, "task_sha": taskSHA, "task": frozen, "reserved_bytes": int64(16777216), "input_digest": sha, "qualification_expected": map[string]any{"Operation": operation, "Version": "1", "Backend": backend}, "qualification_report": map[string]any{"contract": "QualificationReport", "backend": backend}, "qualified_costs": map[string]any{}, "input_objects": []any{}, "source_objects": []any{}, "work_icc_sha": sha}
			check := func(runID uuid.UUID, value any, want bool) {
				t.Helper()
				body, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var got bool
				if err = db.QueryRow(ctx, "SELECT photo.semantic_scene_marker_identity($1,$2)", runID, body).Scan(&got); err != nil || got != want {
					t.Fatalf("%s: want %v got %v error %v", operation, want, got, err)
				}
			}
			check(run, marker, true)
			check(uuid.New(), marker, false)
			for _, key := range []string{"qualification_expected", "qualification_report"} {
				changed := make(map[string]any, len(marker))
				for k, v := range marker {
					changed[k] = v
				}
				other := "mps"
				if backend == "mps" {
					other = "cuda"
				}
				if key == "qualification_expected" {
					changed[key] = map[string]any{"Operation": operation, "Version": "1", "Backend": other}
				} else {
					changed[key] = map[string]any{"contract": "QualificationReport", "backend": other}
				}
				check(run, changed, false)
			}

			for key, value := range map[string]any{"operation": "face.parse", "parent_run_id": uuid.New(), "task_id": uuid.New(), "params_document": uuid.New(), "source_binding_sha": strings.Repeat("b", 64), "source_document_sha": strings.Repeat("b", 64), "auth_session_id": uuid.Nil, "request_sha": nil, "input_objects": nil, "source_objects": map[string]any{}, "unexpected": true} {
				modified := make(map[string]any, len(marker))
				for k, v := range marker {
					modified[k] = v
				}
				modified[key] = value
				check(run, modified, false)
			}
			for _, key := range []string{"source", "params", "qualification_expected", "qualification_report", "qualified_costs", "input_objects", "source_objects", "work_icc_sha", "task", "reserved_bytes"} {
				modified := make(map[string]any, len(marker))
				for k, v := range marker {
					if k != key {
						modified[k] = v
					}
				}
				check(run, modified, false)
			}
		}
	}
}
