package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/workflow"
)

type PerspectiveLinesSubmit interface {
	SubmitProjectIntent(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, workflow.PerspectiveLinesSubmissionIntent) (workflow.PerspectiveLinesSubmissionReceipt, error)
}

func validatePerspectiveLinesSubmissionReceipt(receipt workflow.PerspectiveLinesSubmissionReceipt, who auth.Identity, tenant, project uuid.UUID, key string) error {
	run, err := workflow.PerspectiveLinesSubmissionRun(tenant, project, who, key)
	if err != nil || receipt.Run != run || receipt.Task == uuid.Nil {
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	expected, err := v2input.WorkflowID(run)
	if err != nil || receipt.Workflow != expected {
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	switch receipt.State {
	case "queued", "running", "waiting_review", "completed":
		return nil
	}
	return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
}
func (s *Server) perspectiveLinesSubmissionRoutes(router chi.Router) {
	router.Post("/api/v1/tenants/{t}/projects/{id}/perspective-line-detections", func(w http.ResponseWriter, r *http.Request) {
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
		intent, err := workflow.DecodePerspectiveLinesSubmissionIntent(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.PerspectiveLinesSubmit == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		receipt, err := s.PerspectiveLinesSubmit.SubmitProjectIntent(r.Context(), who, tenant, project, key, intent)
		if err != nil {
			writeError(w, r, perspectiveLinesSubmissionError(err))
			return
		}
		if err = validatePerspectiveLinesSubmissionReceipt(receipt, who, tenant, project, key); err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusAccepted, receipt)
	})
}

func perspectiveLinesSubmissionError(err error) error {
	if errors.Is(err, workflow.ErrPerspectiveLinesDraftExpired) {
		return domain.NewError("PERSPECTIVE_DETECTION_DRAFT_EXPIRED", 410)
	}
	return semanticProposalError(err)
}
