package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/interchange"
)

type InterchangeMaskDecider interface {
	Decide(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, interchange.ImportPersonMaskDecisionRequest) (interchange.ImportPersonMaskDecisionReceipt, error)
}

func serveInterchangeMaskDecision(w http.ResponseWriter, r *http.Request, who auth.Identity, tenant, project, workflow uuid.UUID, decider InterchangeMaskDecider) {
	if decider == nil {
		writeError(w, r, domain.NewError("INTERCHANGE_UNAVAILABLE", 503))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	var req interchange.ImportPersonMaskDecisionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if req.ExpectedEpoch == nil || *req.ExpectedEpoch < 0 || *req.ExpectedEpoch >= 9007199254740991 {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	result, err := decider.Decide(r.Context(), who, tenant, project, workflow, req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if result.WorkflowID != workflow || result.EntryID != req.EntryID || result.Epoch != *req.ExpectedEpoch+1 || result.Epoch < 1 || result.Epoch > 9007199254740991 ||
		(req.Action == "grant" && result.Status != "granted") ||
		(req.Action == "revoke" && result.Status != "revoked") ||
		(req.Action != "grant" && req.Action != "revoke") {
		writeError(w, r, domain.NewError("INTERCHANGE_MASK_DECISION_INVALID_RECEIPT", 503))
		return
	}
	if r.Context().Err()!=nil{return}
	writeJSON(w, http.StatusOK, result)
}
