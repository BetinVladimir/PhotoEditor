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

type RelightSubmit interface {
	SubmitProjectKeyed(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, v2portrait.RelightDraftIntent) ([]byte, error)
}

func relightTicket(raw []byte, who auth.Identity) (semanticProposeTicket, error) {
	var receipt struct {
		Run      uuid.UUID `json:"run_id"`
		Workflow uuid.UUID `json:"workflow_id"`
		Actor    uuid.UUID `json:"actor_id"`
		Session  uuid.UUID `json:"auth_session_id"`
		State    string    `json:"state"`
		Schema   int       `json:"schema_version"`
	}
	normal, canonicalErr := canonical.Bytes(raw)
	if len(raw) > 65536 || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || canonicalErr != nil || json.Unmarshal(normal, &receipt) != nil || receipt.Schema != 1 || receipt.Run == uuid.Nil || receipt.Actor != who.UserID || receipt.Session != who.SessionID || receipt.State != "queued" {
		return semanticProposeTicket{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	expected, err := v2input.WorkflowID(receipt.Run)
	if err != nil || expected != receipt.Workflow {
		return semanticProposeTicket{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return semanticProposeTicket{RunID: receipt.Run, WorkflowID: receipt.Workflow, State: "queued"}, nil
}

func (s *Server) relightRoutes(router chi.Router) {
	s.relightAnalysisRoutes(router)
	s.relightCandidateRoutes(router)
	s.relightPreparationRoutes(router)
	router.Post("/api/v1/tenants/{t}/projects/{id}/relight-intents", func(w http.ResponseWriter, r *http.Request) {
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
		r.Body = http.MaxBytesReader(w, r.Body, v2portrait.MaxRelightSubmissionBytes)
		var raw json.RawMessage
		if err = decodeJSON(r, &raw); err != nil {
			writeError(w, r, err)
			return
		}
		intent, err := v2portrait.DecodeRelightDraftIntent(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.RelightSubmit == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		receipt, err := s.RelightSubmit.SubmitProjectKeyed(r.Context(), who, tenant, project, key, intent)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		ticket, err := relightTicket(receipt, who)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 202, ticket)
	})
}
