package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestExpansionMaterializeWireAndStaleResult(t *testing.T) {
	in, branch, sizes := taskFixture(t)
	task, err := PrepareDefectTask(in.RunID, in.ReviewID, in.Protection.Canvas, branch, sizes)
	if err != nil {
		t.Fatal(err)
	}
	lease := DefectExpansionLease{ExpansionID: in.ExpansionID, Token: uuid.New(), TenantID: in.TenantID, RunID: in.RunID, ReviewID: in.ReviewID, Route: "repair", DecisionHash: in.Resolved.SHA256, Selection: in.Selection}
	q := &expansionQueries{row: expansionRow{values: []any{true}}}
	repo := SQLExpansionLeases{DB: q}
	done, err := repo.Materialize(context.Background(), lease, branch, &task)
	if err != nil || !done {
		t.Fatal(err)
	}
	var body map[string]any
	if err = json.Unmarshal(q.args[2].([]byte), &body); err != nil {
		t.Fatal(err)
	}
	node := body["task"].(map[string]any)
	if _, ok := node["execution_profile"].(map[string]any); !ok {
		t.Fatal("profile encoded as base64 instead of JSON")
	}
	if body["base"] != nil || node["operation"] != "defects.repair" || q.args[0] != in.ExpansionID || q.args[1] != lease.Token {
		t.Fatal("incorrect commit envelope")
	}
	q.row = expansionRow{values: []any{false}}
	if done, err = repo.Materialize(context.Background(), lease, branch, &task); err != nil || done {
		t.Fatal("stale finalize reported success", err)
	}
	savedDigest := task.InputDigest
	task.InputDigest = lease.DecisionHash
	q.args = nil
	if _, err = repo.Materialize(context.Background(), lease, branch, &task); err == nil || q.args != nil {
		t.Fatal("incorrect digest reached DB")
	}
	task.InputDigest = savedDigest
	task.ID = uuid.New()
	q.args = nil
	if _, err = repo.Materialize(context.Background(), lease, branch, &task); err == nil || q.args != nil {
		t.Fatal("wrong task reached DB")
	}
}
