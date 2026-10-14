package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func expansionPlanFixture(t *testing.T) (PreparePlan, ExpansionLease, expansionPlanPayload) {
	t.Helper()
	plan, _, _ := packageAdmissionFixture(t)
	lease := validExpansionLease()
	lease.Workflow, _ = v2input.WorkflowID(lease.Run)
	plan.workflow = lease.Workflow
	plan.task, _ = v2input.TaskID(lease.Workflow, "v2.delivery.prepare", 0)
	lease.ParentTask = plan.task
	lease.Cursor = 0
	lease.SlotCount = plan.preparation.SlotCount
	var proof PrepareProof
	if err := json.Unmarshal(plan.ProofJCS(), &proof); err != nil {
		t.Fatal(err)
	}
	return plan, lease, expansionPlanPayload{Proof: proof, Params: plan.params, Input: plan.input}
}
func TestExpansionLoadPlanRestoresFrozenPageAndBindsLease(t *testing.T) {
	expected, lease, payload := expansionPlanFixture(t)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	db := &deliveryReadTx{raw: raw}
	store := ExpansionLeaseStore{DB: db}
	plan, err := store.LoadPlan(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.args) != 2 || db.args[0] != lease.Expansion || db.args[1] != lease.Token {
		t.Fatal("wrong fence")
	}
	if string(plan.ProofJCS()) != string(expected.ProofJCS()) || plan.TaskID() != expected.TaskID() {
		t.Fatal("frozen proof changed")
	}
	db.raw[0] = 'x'
	page, err := plan.ExportChildren(lease.Run, lease.Cursor, 100)
	if err != nil || !page.Done || len(page.Items) != lease.SlotCount {
		t.Fatal("recovered plan cannot expand", err)
	}
	if _, _, err := plan.ExportRequest(lease.Run, page.Items[0]); err != nil {
		t.Fatal("recovered export intent invalid", err)
	}
}
func TestExpansionLoadPlanRejectsForeignScopeAndSubstitution(t *testing.T) {
	_, lease, payload := expansionPlanFixture(t)
	for _, tc := range []struct {
		name   string
		change func(*ExpansionLease, *expansionPlanPayload)
	}{
		{"task", func(l *ExpansionLease, p *expansionPlanPayload) { l.ParentTask = uuid.New() }},
		{"parent", func(l *ExpansionLease, p *expansionPlanPayload) { l.Run = uuid.New() }},
		{"count", func(l *ExpansionLease, p *expansionPlanPayload) { l.SlotCount++ }},
		{"params", func(l *ExpansionLease, p *expansionPlanPayload) { p.Params.SHA256 = digest([]byte("foreign")) }},
		{"input", func(l *ExpansionLease, p *expansionPlanPayload) { p.Input.SHA256 = digest([]byte("foreign")) }},
		{"snapshot", func(l *ExpansionLease, p *expansionPlanPayload) { p.Proof.SnapshotJCS = "{}" }},
		{"config", func(l *ExpansionLease, p *expansionPlanPayload) { p.Proof.ConfigsJCS = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changedLease, changedPayload := lease, payload
			tc.change(&changedLease, &changedPayload)
			raw, err := json.Marshal(changedPayload)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (ExpansionLeaseStore{DB: &deliveryReadTx{raw: raw}}).LoadPlan(context.Background(), changedLease); err == nil {
				t.Fatal("substitution accepted")
			}
		})
	}
}
func TestExpansionLoadPlanRejectsMissingUnknownAndTrailingPayload(t *testing.T) {
	_, lease, payload := expansionPlanFixture(t)
	raw, _ := json.Marshal(payload)
	for _, bad := range [][]byte{nil, []byte("null"), []byte("{}"), append(append([]byte(nil), raw...), []byte(" {}")...), append([]byte(`{"unknown":true,`), raw[1:]...)} {
		if _, err := (ExpansionLeaseStore{DB: &deliveryReadTx{raw: bad}}).LoadPlan(context.Background(), lease); err == nil {
			t.Fatal("invalid recovery payload accepted")
		}
	}
}
