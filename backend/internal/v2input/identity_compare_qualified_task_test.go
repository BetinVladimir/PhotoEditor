package v2input

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestQualifiedIdentityTaskPinsRuntimeAndPolicy(t *testing.T) {
	params, paramsRef, docs, dataset := identityClosureFixture(t)
	workflow := uuid.New()
	closure, err := CompileIdentityCompareClosure(context.Background(), workflow, 0,
		params, paramsRef, docs, dataset)
	if err != nil {
		t.Fatal(err)
	}
	sizes := make([]SemanticInputSize, len(closure.Direct))
	for i, ref := range closure.Direct {
		sizes[i] = SemanticInputSize{Ref: ref, Bytes: 4096}
	}
	pin := IdentityQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(),
		Report:      Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)},
		Calibration: params.Calibration, ModelLockSHA: params.ModelLockSHA,
		RuntimeLockSHA: strings.Repeat("b", 64), DeviceProfileSHA: strings.Repeat("c", 64),
		DatasetSHA: dataset, Backend: "cpu"}
	costs := SemanticQualifiedCosts{RAMBytes: 256 << 20, ScratchBytes: 64 << 20}
	frozen, err := CompileQualifiedIdentityCompareTask(context.Background(), workflow, 0,
		params, paramsRef, docs, sizes, costs, pin, 32<<30, 0, 12<<30, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]json.RawMessage
	if json.Unmarshal(draft.ExecutionProfile, &profile) != nil {
		t.Fatal("profile")
	}
	for key, want := range map[string]string{"model_lock_sha": pin.ModelLockSHA,
		"runtime_lock_sha": pin.RuntimeLockSHA, "device_profile_sha": pin.DeviceProfileSHA,
		"dataset_sha": pin.DatasetSHA} {
		var got string
		if json.Unmarshal(profile[key], &got) != nil || got != want {
			t.Fatal("missing pin", key)
		}
	}
	changed := pin
	changed.ModelLockSHA = strings.Repeat("e", 64)
	if _, err := CompileQualifiedIdentityCompareTask(context.Background(), workflow, 0,
		params, paramsRef, docs, sizes, costs, changed, 32<<30, 0, 12<<30, 1<<30); err == nil {
		t.Fatal("foreign model pin accepted")
	}
	changed = pin
	changed.Calibration = changed.Report
	if _, err := CompileQualifiedIdentityCompareTask(context.Background(), workflow, 0,
		params, paramsRef, docs, sizes, costs, changed, 32<<30, 0, 12<<30, 1<<30); err == nil {
		t.Fatal("foreign calibration pin accepted")
	}
}

func TestResolvedIdentityTaskRejectsForeignQueryBeforeCatalog(t *testing.T) {
	params, paramsRef, documents, _ := identityClosureFixture(t)
	query := IdentityQualificationQuery{Tenant: uuid.New(), Project: uuid.New(),
		Backend: "cpu", ModelLockSHA: strings.Repeat("f", 64),
		Calibration: params.Calibration}
	catalog := &identityQualificationCatalogFixture{}
	_, err := CompileResolvedIdentityCompareTask(context.Background(), uuid.New(),
		0, query, params, paramsRef, documents, nil, SemanticQualifiedCosts{},
		32<<30, 0, 12<<30, 1<<30, catalog, sessionImageBytes(nil))
	if err != ErrInvalid || catalog.reads != 0 {
		t.Fatalf("foreign model query reached catalog: %v", err)
	}
}
