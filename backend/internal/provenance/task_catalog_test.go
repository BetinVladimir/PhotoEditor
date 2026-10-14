package provenance

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestCanonicalSigningTaskNormalizesOnlyDatabaseJSON(t *testing.T) {
	for _, mode := range []string{"valid", "task", "result", "attempt", "fence", "operation", "params", "digest", "inputs", "outputs", "metrics", "duplicate", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			input := json.RawMessage(`[ { "sha256": "a", "artifact_id": "b" } ]`)
			value := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "encode", Version: "1", InputDigest: strings.Repeat("a", 64), ResultDigest: strings.Repeat("b", 64), Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}, Inputs: input, Outputs: json.RawMessage(`[]`), Metrics: json.RawMessage(`{ "z":1,"a":2 }`)}
			switch mode {
			case "task":
				value.Task = uuid.Nil
			case "result":
				value.Result = uuid.Nil
			case "attempt":
				value.Attempt = 0
			case "fence":
				value.Fence = 0
			case "operation":
				value.Operation = " encode"
			case "params":
				value.Params.ArtifactID = uuid.Nil
			case "digest":
				value.ResultDigest = "invalid"
			case "inputs":
				value.Inputs = json.RawMessage(`{}`)
			case "outputs":
				value.Outputs = json.RawMessage(`null`)
			case "metrics":
				value.Metrics = json.RawMessage(`[]`)
			case "duplicate":
				value.Metrics = json.RawMessage(`{"a":1,"a":2}`)
			case "oversize":
				value.Metrics = json.RawMessage(strings.Repeat(" ", (8<<20)+1))
			}
			err := normalizeCanonicalSigningTask(&value)
			if mode != "valid" {
				if err == nil {
					t.Fatal("invalid task evidence accepted")
				}
				return
			}
			if err != nil || string(value.Metrics) != `{"a":2,"z":1}` {
				t.Fatal("database normalization failed", err)
			}
			input[0] = 'x'
			if value.Inputs[0] != '[' {
				t.Fatal("borrowed database buffer")
			}
		})
	}
}

func TestSigningTaskCatalogFailsBeforeDatabaseIO(t *testing.T) {
	source := SQLSigningTaskCatalog{}
	if _, err := source.ReadCanonicalSigningTask(context.Background(), auth.Identity{}, ReadySigningExport{}, uuid.New()); err == nil {
		t.Fatal("unconfigured catalog accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.ReadCanonicalSigningTask(ctx, auth.Identity{}, ReadySigningExport{}, uuid.New()); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
