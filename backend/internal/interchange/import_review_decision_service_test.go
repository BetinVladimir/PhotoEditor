package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestImportReviewDecisionServiceRejectsInvalidRequestBeforeDatabase(t *testing.T) {
	service := ImportReviewDecisionService{}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenantID, projectID, workflowID, requestID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err := service.Decide(context.Background(), who, tenantID, projectID, workflowID, requestID, ImportReviewDecisionRequest{Action: "grant"})
	if err == nil {
		t.Fatal("unavailable decision service accepted a request")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.Decide(ctx, who, tenantID, projectID, workflowID, requestID, ImportReviewDecisionRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request: %v", err)
	}
}

func TestImportReviewSavedListRequiresCurrentService(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	_, err := (ImportReviewDecisionService{}).ListSaved(context.Background(), who, uuid.New(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("uninstalled saved review list accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (ImportReviewDecisionService{}).ListSaved(ctx, who, uuid.New(), uuid.New(), uuid.New())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled saved list: %v", err)
	}
}

func TestImportReviewDecisionRequestRejectsClientAuthorityFields(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	}
	request := ImportReviewDecisionRequest{Action: "grant", Kind: "dust", EntryID: uuid.New(), TargetVersionID: uuid.New(), Proposal: ref(), Base: ref(), Decision: "accept"}
	if err := validateImportReviewDecisionRequest(context.Background(), request); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	request.Accepted = []string{"zz"}
	if err := validateImportReviewDecisionRequest(context.Background(), request); err == nil {
		t.Fatal("invalid candidate id accepted")
	}
}
