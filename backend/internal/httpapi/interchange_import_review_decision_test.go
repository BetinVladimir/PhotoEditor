package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/interchange"
)

type importReviewDecisionStub struct{ called bool }

func (s *importReviewDecisionStub) ListSaved(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID) ([]interchange.ImportReviewDecisionStatus, error) {
	s.called = true
	return nil, nil
}

func (s *importReviewDecisionStub) ListSavedPage(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, string) (interchange.ImportReviewSavedPage, error) {
	s.called = true
	return interchange.ImportReviewSavedPage{Items: []interchange.ImportReviewDecisionStatus{}}, nil
}

func (s *importReviewDecisionStub) Decide(_ context.Context, _ auth.Identity, _, _, workflow, requestID uuid.UUID, req interchange.ImportReviewDecisionRequest) (interchange.ImportReviewDecisionReceipt, error) {
	s.called = true
	return interchange.ImportReviewDecisionReceipt{WorkflowID: workflow, EntryID: req.EntryID, Kind: req.Kind, TargetVersionID: req.TargetVersionID, ProposalID: req.Proposal.ArtifactID, Status: "revoked", Epoch: req.ExpectedEpoch + 1}, nil
}

func (s *importReviewDecisionStub) ReadStatus(_ context.Context, _ auth.Identity, _, _, _, entry, target uuid.UUID, kind string, proposal uuid.UUID) (interchange.ImportReviewDecisionStatus, error) {
	s.called = true
	return interchange.ImportReviewDecisionStatus{EntryID: entry, TargetVersionID: target, Kind: kind, Status: "revoked", Epoch: 2}, nil
}

func TestImportReviewDecisionHTTPRequiresRequestID(t *testing.T) {
	stub := &importReviewDecisionStub{}
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"action":"revoke"}`))
	response := httptest.NewRecorder()
	serveImportReviewDecision(response, request, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), stub)
	if response.Code != http.StatusUnprocessableEntity || stub.called {
		t.Fatalf("invalid id reached service: %d", response.Code)
	}
}
