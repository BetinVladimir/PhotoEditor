package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/interchange"
)

type InterchangeImportReviewDecider interface {
	Decide(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, interchange.ImportReviewDecisionRequest) (interchange.ImportReviewDecisionReceipt, error)
	ReadStatus(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, string, uuid.UUID) (interchange.ImportReviewDecisionStatus, error)
	ListSaved(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID) ([]interchange.ImportReviewDecisionStatus, error)
	ListSavedPage(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, string) (interchange.ImportReviewSavedPage, error)
}

func serveImportReviewDecision(w http.ResponseWriter, r *http.Request, who auth.Identity, tenantID, projectID, workflowID uuid.UUID, decider InterchangeImportReviewDecider) {
	if decider == nil {
		writeError(w, r, domain.NewError("INTERCHANGE_UNAVAILABLE", 503))
		return
	}
	requestID, err := uuid.Parse(r.Header.Get("Idempotency-Key"))
	if err != nil || requestID == uuid.Nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req interchange.ImportReviewDecisionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := decider.Decide(r.Context(), who, tenantID, projectID, workflowID, requestID, req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if result.WorkflowID != workflowID || result.EntryID != req.EntryID || result.TargetVersionID != req.TargetVersionID ||
		result.Kind != req.Kind || result.ProposalID != req.Proposal.ArtifactID || result.Epoch < 1 || result.Epoch > 9007199254740991 || result.Epoch != req.ExpectedEpoch+1 ||
		(req.Action != "grant" && req.Action != "revoke") ||
		(req.Action == "grant" && result.Status != "granted") || (req.Action == "revoke" && result.Status != "revoked") {
		writeError(w, r, domain.NewError("INTERCHANGE_DECISION_INVALID_RECEIPT", 503))
		return
	}
	if r.Context().Err()!=nil{return}
	writeJSON(w, http.StatusOK, result)

}

func serveImportReviewStatus(w http.ResponseWriter, r *http.Request, who auth.Identity, tenantID, projectID, workflowID, entryID, targetID uuid.UUID, kind string, proposalID uuid.UUID, decider InterchangeImportReviewDecider) {
	if decider == nil {
		writeError(w, r, domain.NewError("INTERCHANGE_UNAVAILABLE", 503))
		return
	}
	result, err := decider.ReadStatus(r.Context(), who, tenantID, projectID, workflowID, entryID, targetID, kind, proposalID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if result.EntryID != entryID || result.TargetVersionID != targetID || result.Kind != kind || result.Proposal.ArtifactID != proposalID || result.Epoch < 1 {
		writeError(w, r, domain.NewError("INTERCHANGE_DECISION_INVALID_RECEIPT", 503))
		return
	}
	if r.Context().Err()!=nil{return}
	writeJSON(w, http.StatusOK, result)
}
