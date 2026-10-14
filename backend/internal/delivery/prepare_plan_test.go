package delivery

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPreparePlanPinsDerivedRootsAndOwnsRecoveryBytes(t *testing.T) {
	snapshot, snapshotRaw, policy, configs := freezeFixture(t)
	_, frozenRaw, sha, err := CompileFrozenInput(snapshot, snapshotRaw, policy, configs)
	if err != nil {
		t.Fatal(err)
	}
	frozen := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	project, session, workflow := uuid.New(), uuid.New(), uuid.New()
	_, _, params, err := CompilePrepareProof(project, session, frozen, frozenRaw, snapshotRaw, configs)
	if err != nil {
		t.Fatal(err)
	}
	paramsRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(params)}
	plan, err := CompilePreparePlan(workflow, project, session, paramsRef, frozen, frozenRaw, snapshotRaw, configs)
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := v2input.TaskID(workflow, "v2.delivery.prepare", 0)
	if plan.TaskID() != expected || plan.NodeKey() != "v2.delivery.prepare:0" {
		t.Fatal("wrong identity")
	}
	roots := plan.Roots()
	if len(roots) != 5 {
		t.Fatalf("root count = %d", len(roots))
	}
	if _, err := plan.InputClosure(roots); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.InputClosure(roots[:len(roots)-1]); err == nil {
		t.Fatal("missing recipe accepted")
	}
	roots[0] = v2input.Entry{}
	bytes := plan.ParamsJCS()
	bytes[0] = 'x'
	proof := plan.ProofJCS()
	proof[0] = 'x'
	frozenRaw[0] = 'x'
	snapshotRaw[0] = 'x'
	configs[0].Raw[0] = 'x'
	if _, err := plan.InputClosure(plan.Roots()); err != nil {
		t.Fatal("borrowed state", err)
	}
	if plan.ParamsJCS()[0] != '{' || plan.ProofJCS()[0] != '{' {
		t.Fatal("borrowed recovery bytes")
	}
	if _, err := plan.PublishInputs(context.Background(), plan.Roots(), nil); err == nil {
		t.Fatal("nil publisher accepted")
	}
	if _, err := (PreparePlan{}).InputClosure(nil); err == nil {
		t.Fatal("zero plan accepted")
	}
}
