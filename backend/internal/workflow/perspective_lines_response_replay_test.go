package workflow

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPerspectiveLinesStoredResponseRequiresOriginalAcceptanceScope(t *testing.T) {
	run := uuid.New()
	graph, _ := v2input.WorkflowID(run)
	original := PerspectiveLinesSubmissionReceipt{Run: run, Workflow: graph, Task: uuid.New(), State: "queued"}
	body, err := canonical.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	first, err := parsePerspectiveLinesStoredResponse(body, run)
	if err != nil || first == nil || *first != original {
		t.Fatal(first, err)
	}
	first.State = "running"
	replay, err := parsePerspectiveLinesStoredResponse(body, run)
	if err != nil || replay == nil || replay.State != "queued" {
		t.Fatal("mutated original receipt", err)
	}
	for _, change := range []func(*PerspectiveLinesSubmissionReceipt){
		func(r *PerspectiveLinesSubmissionReceipt) { r.Run = uuid.New() }, func(r *PerspectiveLinesSubmissionReceipt) { r.Workflow = uuid.New() }, func(r *PerspectiveLinesSubmissionReceipt) { r.Task = uuid.Nil }, func(r *PerspectiveLinesSubmissionReceipt) { r.State = "running" },
	} {
		bad := original
		change(&bad)
		raw, _ := canonical.Marshal(bad)
		if _, err := parsePerspectiveLinesStoredResponse(raw, run); err == nil {
			t.Fatal("forged saved response accepted")
		}
	}
	for _, raw := range [][]byte{nil, []byte("null")} {
		if response, err := parsePerspectiveLinesStoredResponse(raw, run); err != nil || response != nil {
			t.Fatal("pending response rejected", err)
		}
	}
	extra, _ := canonical.Marshal(map[string]any{"run_id": run, "workflow_id": graph, "task_id": original.Task, "state": "queued", "extra": true})
	if _, err := parsePerspectiveLinesStoredResponse(extra, run); err == nil {
		t.Fatal("unexpected response fields accepted")
	}
}
