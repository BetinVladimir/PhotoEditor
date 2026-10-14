package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2draft"
)

type OpenEyesPrepare interface {
	PrepareProjectKeyed(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string,
		v2draft.OpenEyesClientSelection) (v2draft.OpenEyesClientPreparation, error)
}

type OpenEyesSubmit interface {
	SubmitProjectKeyed(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string,
		v2draft.OpenEyesClientIntent) (v2draft.DonorQueuedTicket, error)
}

type OpenEyesService interface {
	OpenEyesPrepare
	OpenEyesSubmit
	GlareSourceCandidates
	GlareDonorCandidates
	GlareMaskPairs
}

func (s *Server) openEyesRoutes(router chi.Router) {
	router.Post("/api/v1/tenants/{t}/projects/{id}/open-eyes-source-candidates",
		func(w http.ResponseWriter, r *http.Request) {
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
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			var raw json.RawMessage
			if err = decodeJSON(r, &raw); err != nil {
				writeError(w, r, err)
				return
			}
			query, err := v2draft.DecodeGlareSourceCandidateQuery(raw)
			if err != nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			if s.OpenEyesSourceCandidates == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			page, err := s.OpenEyesSourceCandidates.ListProjectSourceCandidates(r.Context(), who, tenant, project, query)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			writeJSON(w, http.StatusOK, page)
		})
	router.Post("/api/v1/tenants/{t}/projects/{id}/open-eyes-donor-candidates",
		func(w http.ResponseWriter, r *http.Request) {
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
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			var raw json.RawMessage
			if err = decodeJSON(r, &raw); err != nil {
				writeError(w, r, err)
				return
			}
			query, err := v2draft.DecodeGlareDonorCandidateQuery(raw)
			if err != nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			if s.OpenEyesDonorCandidates == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			page, err := s.OpenEyesDonorCandidates.ListProjectCandidates(r.Context(), who, tenant, project, query)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			writeJSON(w, http.StatusOK, page)
		})
	router.Post("/api/v1/tenants/{t}/projects/{id}/open-eyes-mask-pairs",
		func(w http.ResponseWriter, r *http.Request) {
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
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			var raw json.RawMessage
			if err = decodeJSON(r, &raw); err != nil {
				writeError(w, r, err)
				return
			}
			query, err := v2draft.DecodeGlareMaskPairQuery(raw)
			if err != nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			if s.OpenEyesMaskPairs == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			page, err := s.OpenEyesMaskPairs.ListProjectMaskPairs(r.Context(), who, tenant, project, query)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			writeJSON(w, http.StatusOK, page)
		})
	router.Post("/api/v1/tenants/{t}/projects/{id}/open-eyes-preparations",
		func(w http.ResponseWriter, r *http.Request) {
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
			r.Body = http.MaxBytesReader(w, r.Body, 16384)
			var raw json.RawMessage
			if err = decodeJSON(r, &raw); err != nil {
				writeError(w, r, err)
				return
			}
			selection, err := v2draft.DecodeOpenEyesClientSelection(raw)
			if err != nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			if s.OpenEyesPrepare == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			prepared, err := s.OpenEyesPrepare.PrepareProjectKeyed(r.Context(), who,
				tenant, project, key, selection)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			writeJSON(w, http.StatusOK, prepared)
		})
	router.Post("/api/v1/tenants/{t}/projects/{id}/open-eyes-intents",
		func(w http.ResponseWriter, r *http.Request) {
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
			r.Body = http.MaxBytesReader(w, r.Body, v2draft.MaxOpenEyesIntentBytes)
			var raw json.RawMessage
			if err = decodeJSON(r, &raw); err != nil {
				writeError(w, r, err)
				return
			}
			intent, err := v2draft.DecodeOpenEyesClientIntent(raw)
			if err != nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			if s.OpenEyesSubmit == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			ticket, err := s.OpenEyesSubmit.SubmitProjectKeyed(r.Context(), who,
				tenant, project, key, intent)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			if ticket.RunID == uuid.Nil || ticket.WorkflowID == uuid.Nil ||
				ticket.TaskID == uuid.Nil || ticket.State != "queued" {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			writeJSON(w, http.StatusAccepted, ticket)
		})
}
