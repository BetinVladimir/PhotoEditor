package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"os"
	"testing"
)

func TestExposureInputBatchRejectsBudgetCoverageBeforeSQLOrIO(t *testing.T) {
	raw, err := os.ReadFile("../../../resources/icc/work_rec2020_gamma24_v1.icc")
	if err != nil {
		t.Fatal(err)
	}
	color, err := NewExposureColorProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	prepared, targets, request := decisionProofKindFixture(t, "local_exposure")
	edits, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil || len(edits) != 1 {
		t.Fatal(err)
	}
	for _, budgets := range [][]ExposureInputBudget{nil, {{TargetID: uuid.New()}}, {{TargetID: edits[0].TargetID}, {TargetID: edits[0].TargetID}}} {
		q := &protectionDocumentQuery{}
		store := &planStoreFixture{}
		inputs, err := ReadApprovedExposureInputBatch(context.Background(), q, store, "fixture", uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), prepared, targets, request, budgets, color)
		if err == nil || inputs != nil || q.calls != 0 || len(store.Opened) != 0 {
			t.Fatal("invalid coverage caused IO")
		}
	}
	missingColorDB := &protectionDocumentQuery{}
	missingColorStore := &planStoreFixture{}
	if inputs, err := ReadApprovedExposureInputBatch(context.Background(), missingColorDB, missingColorStore, "fixture", uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), prepared, targets, request, []ExposureInputBudget{{TargetID: edits[0].TargetID}}, ExposureColorProfile{}); err == nil || inputs != nil || missingColorDB.calls != 0 || len(missingColorStore.Opened) != 0 {
		t.Fatal("missing ICC caused SQL or IO")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q := &protectionDocumentQuery{}
	store := &planStoreFixture{}
	if inputs, err := ReadApprovedExposureInputBatch(ctx, q, store, "fixture", uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), prepared, targets, request, nil, color); err != context.Canceled || inputs != nil || q.calls != 0 {
		t.Fatal("cancelled resolver ran")
	}
}
