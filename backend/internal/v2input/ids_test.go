package v2input

import (
	"testing"

	"github.com/google/uuid"
)

func TestStableAdmissionAndTaskIdentities(t *testing.T) {
	tenant, actor := uuid.New(), uuid.New()
	first, err := RunID(tenant, actor, "same-key")
	if err != nil || first.Version() != 5 {
		t.Fatalf("invalid run id: %s %v", first, err)
	}
	again, _ := RunID(tenant, actor, "same-key")
	other, _ := RunID(tenant, actor, "other-key")
	if again != first || other == first {
		t.Fatal("run id is not stable per idempotency key")
	}
	workflow, _ := WorkflowID(first)
	task, _ := TaskID(workflow, "session.describe", 127)
	if workflow.Version() != 5 || task.Version() != 5 ||
		!validTaskIdentity(workflow, task, "session.describe:127") ||
		validTaskIdentity(workflow, task, "session.describe:0127") {
		t.Fatal("task identity is not canonical")
	}
	if _, err := NodeKey("preview", 5000); err == nil {
		t.Fatal("out-of-range task ordinal accepted")
	}
}
