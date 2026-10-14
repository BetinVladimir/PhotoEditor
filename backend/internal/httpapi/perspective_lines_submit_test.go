package httpapi

import (
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/workflow"
	"testing"
)

func TestPerspectiveLinesSubmissionReceiptScopeAndAdmission(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project := uuid.New(), uuid.New()
	key := "perspective-lines-submission-0001"
	run, err := workflow.PerspectiveLinesSubmissionRun(tenant, project, who, key)
	if err != nil {
		t.Fatal(err)
	}
	graph, _ := v2input.WorkflowID(run)
	for _, state := range []string{"queued", "running", "waiting_review", "completed", "draft", "sealed", "failed", "cancelled"} {
		receipt := workflow.PerspectiveLinesSubmissionReceipt{Run: run, Workflow: graph, Task: uuid.New(), State: state}
		err := validatePerspectiveLinesSubmissionReceipt(receipt, who, tenant, project, key)
		accepted := state == "queued" || state == "running" || state == "waiting_review" || state == "completed"
		if (err == nil) != accepted {
			t.Fatal(state, err)
		}
	}
	receipt := workflow.PerspectiveLinesSubmissionReceipt{Run: run, Workflow: graph, Task: uuid.New(), State: "queued"}
	if err := validatePerspectiveLinesSubmissionReceipt(receipt, who, tenant, uuid.New(), key); err == nil {
		t.Fatal("foreign project accepted")
	}
	receipt.Workflow = uuid.New()
	if err := validatePerspectiveLinesSubmissionReceipt(receipt, who, tenant, project, key); err == nil {
		t.Fatal("foreign workflow accepted")
	}
	receipt.Workflow = graph
	receipt.Task = uuid.Nil
	if err := validatePerspectiveLinesSubmissionReceipt(receipt, who, tenant, project, key); err == nil {
		t.Fatal("empty task accepted")
	}
}

func TestPerspectiveLinesExpiryRequiresDedicatedProof(t *testing.T) {
	for _, item := range []struct {
		cause error
		code  string
	}{
		{workflow.ErrPerspectiveLinesDraftExpired, "PERSPECTIVE_DETECTION_DRAFT_EXPIRED"},
		{v2input.ErrExpired, "SNAPSHOT_EXPIRED"},
	} {
		public := domain.AsError(perspectiveLinesSubmissionError(fmt.Errorf("wrapped: %w", item.cause)))
		if public.Code != item.code || public.HTTPStatus != 410 {
			t.Fatal(public)
		}
	}
}
