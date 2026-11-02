package v2workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/idempotency"
)

func validReceipt(workflow, run uuid.UUID, state string, outputs []Ref, errCode *string) idempotency.Response {
	body := Status{
		WorkflowID: workflow,
		RunID:      run,
		Revision:   "2",
		State:      state,
		Outputs:    outputs,
		ErrorCode:  errCode,
	}
	raw, _ := json.Marshal(body)
	return idempotency.Response{Status: 200, ResourceID: &workflow, Body: raw}
}

func validSHA() string { return strings.Repeat("a", 64) }

// TestCancelReceiptValidStatesCancelledCompletedFailed verifies all four
// allowed states are accepted (the existing test only uses "cancelling").
func TestCancelReceiptValidStatesCancelledCompletedFailed(t *testing.T) {
	workflow, run := uuid.New(), uuid.New()
	for _, state := range []string{"cancelled", "completed", "failed"} {
		var errCode *string
		if state == "failed" {
			s := "ERROR_CODE"
			errCode = &s
		}
		out := validReceipt(workflow, run, state, []Ref{}, errCode)
		if err := validateCancelReceipt(out, workflow); err != nil {
			t.Fatalf("state %q must be accepted, got %v", state, err)
		}
	}
}

// TestCancelReceiptValidOutputsLoop covers the loop body with a valid output.
func TestCancelReceiptValidOutputsLoop(t *testing.T) {
	workflow, run := uuid.New(), uuid.New()
	outputs := []Ref{
		{ArtifactID: uuid.New(), SHA256: validSHA()},
		{ArtifactID: uuid.New(), SHA256: validSHA()},
	}
	out := validReceipt(workflow, run, "cancelled", outputs, nil)
	if err := validateCancelReceipt(out, workflow); err != nil {
		t.Fatalf("valid outputs must be accepted, got %v", err)
	}
}

// TestCancelReceiptNilArtifactIDInOutputs covers the uuid.Nil guard in the loop.
func TestCancelReceiptNilArtifactIDInOutputs(t *testing.T) {
	workflow, run := uuid.New(), uuid.New()
	outputs := []Ref{{ArtifactID: uuid.Nil, SHA256: validSHA()}}
	out := validReceipt(workflow, run, "cancelled", outputs, nil)
	if validateCancelReceipt(out, workflow) == nil {
		t.Fatal("nil ArtifactID in output must be rejected")
	}
}

// TestCancelReceiptDuplicateArtifactIDInOutputs covers the seen[] duplicate guard.
func TestCancelReceiptDuplicateArtifactIDInOutputs(t *testing.T) {
	workflow, run := uuid.New(), uuid.New()
	art := uuid.New()
	outputs := []Ref{
		{ArtifactID: art, SHA256: validSHA()},
		{ArtifactID: art, SHA256: validSHA()},
	}
	out := validReceipt(workflow, run, "cancelled", outputs, nil)
	if validateCancelReceipt(out, workflow) == nil {
		t.Fatal("duplicate ArtifactID in outputs must be rejected")
	}
}

// TestCancelReceiptBadSHA256InOutputs covers the ParseDigest error path in the loop.
func TestCancelReceiptBadSHA256InOutputs(t *testing.T) {
	workflow, run := uuid.New(), uuid.New()
	outputs := []Ref{{ArtifactID: uuid.New(), SHA256: "not-a-valid-sha256"}}
	out := validReceipt(workflow, run, "cancelled", outputs, nil)
	if validateCancelReceipt(out, workflow) == nil {
		t.Fatal("bad SHA256 in output must be rejected")
	}
}

// TestCancelReceiptTooManyOutputs covers the len(value.Outputs)>128 guard.
func TestCancelReceiptTooManyOutputs(t *testing.T) {
	workflow, run := uuid.New(), uuid.New()
	outputs := make([]Ref, 129)
	for i := range outputs {
		outputs[i] = Ref{ArtifactID: uuid.New(), SHA256: validSHA()}
	}
	out := validReceipt(workflow, run, "cancelled", outputs, nil)
	if validateCancelReceipt(out, workflow) == nil {
		t.Fatal("more than 128 outputs must be rejected")
	}
}

// TestCancelReceiptMissingRequiredField covers the fields loop return-invalid path.
func TestCancelReceiptMissingRequiredField(t *testing.T) {
	workflow := uuid.New()
	// Build JSON manually without the "revision" field.
	raw := []byte(`{"run_id":"` + uuid.New().String() + `","workflow_id":"` + workflow.String() + `","state":"cancelled","outputs":[],"error_code":null}`)
	out := idempotency.Response{Status: 200, ResourceID: &workflow, Body: raw}
	if validateCancelReceipt(out, workflow) == nil {
		t.Fatal("missing required field must be rejected")
	}
}
