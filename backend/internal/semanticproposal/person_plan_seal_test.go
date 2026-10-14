package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestPersonPlanSealRetainsAllAdmissionInputs(t *testing.T) {
	// This tests fingerprint ownership only; it is not an admitted draft fixture.
	plan := personDraftPlan{requestKey: "key", key: "child", prepared: sourcePersonPreparation{parent: uuid.New(), document: uuid.New(), catalog: catalogPersonPreparation{inputs: "[]"}, source: sourcePreparationProof{pins: "[]"}}, shell: v2input.DraftShell{Bytes: 1024}}
	var err error
	plan.seal, err = personPlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyPersonPlanSeal(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"costs", "quota", "request", "parent", "document", "catalog", "source", "policy", "qualification"} {
		bad := plan
		switch kind {
		case "costs":
			bad.prepared.costs.VRAMBytes++
		case "quota":
			bad.shell.Bytes++
		case "request":
			bad.requestKey += "other"
		case "parent":
			bad.prepared.parent = uuid.New()
		case "document":
			bad.prepared.document = uuid.New()
		case "catalog":
			bad.prepared.catalog.inputs = "{}"
		case "source":
			bad.prepared.source.pins = "{}"
		case "policy":
			bad.policy.body = "{}"
		case "qualification":
			bad.prepared.qualification.taskSHA = "other"
		}
		if err := verifyPersonPlanSeal(context.Background(), bad); !errors.Is(err, v2input.ErrConflict) {
			t.Fatal("plan mutation accepted", kind, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyPersonPlanSeal(ctx, plan); err != context.Canceled {
		t.Fatal(err)
	}
}
