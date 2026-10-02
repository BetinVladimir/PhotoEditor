package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"testing"
)

func TestIngestTaskMaterializationWire(t *testing.T) {
	spec, registry := ingestFixture(t)
	plan, _ := CompileIngest(spec, registry)
	node := plan.Nodes[0]
	sum := sha256.Sum256(node.Params)
	task, err := PrepareIngestTask(node, review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, 2345, nil)
	if err != nil {
		t.Fatal(err)
	}
	q := &expansionQueries{row: expansionRow{values: []any{true}}}
	repo := IngestPlanRepository{DB: q}
	tenant := uuid.New()
	saved, err := repo.Materialize(context.Background(), tenant, spec.RunID, task, nil)
	if err != nil || !saved || q.args[0] != tenant || q.args[1] != spec.RunID {
		t.Fatal("materialize failed", err)
	}
	var wire map[string]json.RawMessage
	if json.Unmarshal(q.args[2].([]byte), &wire) != nil || len(wire["execution_profile"]) == 0 || wire["execution_profile"][0] != '{' {
		t.Fatal("profile encoded as string")
	}
	q.row = expansionRow{values: []any{false}}
	if saved, err = repo.Materialize(context.Background(), tenant, spec.RunID, task, nil); err != nil || saved {
		t.Fatal("stale task accepted")
	}
	metadata := uuid.New()
	q.args = nil
	if _, err = repo.Materialize(context.Background(), tenant, spec.RunID, task, &metadata); err == nil || q.args != nil {
		t.Fatal("verify accepted metadata")
	}
	task.InputDigest = "bad"
	if _, err = repo.Materialize(context.Background(), tenant, spec.RunID, task, nil); err == nil {
		t.Fatal("wrong digest accepted")
	}
}
