package workflow

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestPerspectiveLinesSubmissionStatesRejectForgedAndUnadmittedReceipts(t *testing.T) {
	task := uuid.New()
	for _, stage := range []string{"draft", "sealed", "queued", "running", "waiting_review", "completed", "expired", "draft_expired"} {
		raw, _ := canonical.Marshal(map[string]any{"stage": stage, "task_id": task})
		state, err := parsePerspectiveLinesSubmissionState(raw)
		if err != nil || state.Task == nil || *state.Task != task {
			t.Fatal(stage, err)
		}
		_, err = perspectiveLinesSubmissionReceipt(uuid.New(), state)
		admitted := stage == "queued" || stage == "running" || stage == "waiting_review" || stage == "completed"
		if (err == nil) != admitted {
			t.Fatal("unadmitted receipt accepted", stage, err)
		}
	}
	for _, raw := range []string{`null`, `{}`, `{"stage":"queued","task_id":null}`, `{"stage":"missing","task_id":"` + task.String() + `"}`, `{"stage":"unknown","task_id":null}`, `{"stage":"missing","task_id":null,"queued":true}`} {
		if _, err := parsePerspectiveLinesSubmissionState([]byte(raw)); err == nil {
			t.Fatal("forged state accepted", raw)
		}
	}
	for _, stage := range []string{"missing", "conflict"} {
		raw, _ := canonical.Marshal(map[string]any{"stage": stage, "task_id": nil})
		if _, err := parsePerspectiveLinesSubmissionState(raw); err != nil {
			t.Fatal(stage, err)
		}
	}
}

func TestPerspectiveLinesBindingStatusIsFailClosed(t *testing.T) {
	if err := perspectiveLinesBindingStatus("bound"); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"conflict", "expired", "", "queued", "ok"} {
		if err := perspectiveLinesBindingStatus(status); err == nil {
			t.Fatal("unbound request accepted", status)
		}
	}
}
