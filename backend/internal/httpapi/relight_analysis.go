package httpapi

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type RelightAnalysisSubmit interface {
	SubmitProjectAnalysis(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, v2portrait.RelightAnalysisIntent) (v2portrait.RelightAnalysisReceipt, error)
}

func analysisJobTicket(receipt v2portrait.RelightAnalysisReceipt, who auth.Identity, intent v2portrait.RelightAnalysisIntent) (semanticProposeTicket, error) {
	raw, err := canonical.Marshal(receipt)
	if err != nil {
		return semanticProposeTicket{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	ticket, err := relightTicket(raw, who)
	if err != nil {
		return ticket, err
	}
	task, err := v2input.TaskID(ticket.WorkflowID, intent.Operation, 0)
	if err != nil || receipt.Operation != intent.Operation || receipt.Task != task {
		return semanticProposeTicket{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return ticket, nil
}
func (s *Server) relightAnalysisRoutes(router chi.Router) {
	router.Post("/api/v1/tenants/{t}/projects/{id}/relight-analyses", func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		project, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil || project == uuid.Nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !idempotency.ValidKey(key) {
			writeError(w, r, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		var raw json.RawMessage
		if err = decodeJSON(r, &raw); err != nil {
			writeError(w, r, err)
			return
		}
		intent, err := v2portrait.DecodeRelightAnalysisIntent(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.RelightAnalysis == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		receipt, err := s.RelightAnalysis.SubmitProjectAnalysis(r.Context(), who, tenant, project, key, intent)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		ticket, err := analysisJobTicket(receipt, who, intent)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 202, ticket)
	})
}
