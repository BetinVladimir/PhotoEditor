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

type GlareDonorSubmit interface {
	SubmitProjectKeyed(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string,
		v2draft.GlareDonorClientIntent) (v2draft.RestorationAdmissionTicket, error)
}

type GlareDonorPrepare interface {
	PrepareProjectKeyed(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string,
		v2draft.GlareDonorPreparationRequest) (v2draft.GlareDonorClientIntent, error)
}

type GlareDonorService interface {
	GlareDonorSubmit
	GlareDonorPrepare
	GlareDonorCandidates
	GlareSourceCandidates
	GlareMaskPairs
}

type GlareMaskPairs interface {
	ListProjectMaskPairs(context.Context, auth.Identity, uuid.UUID, uuid.UUID,
		v2draft.GlareMaskPairQuery) (v2draft.GlareMaskPairPage, error)
}

type GlareDonorCandidates interface {
	ListProjectCandidates(context.Context, auth.Identity, uuid.UUID, uuid.UUID,
		v2draft.GlareDonorCandidateQuery) (v2draft.GlareDonorCandidatePage, error)
}

type GlareSourceCandidates interface {
	ListProjectSourceCandidates(context.Context, auth.Identity, uuid.UUID, uuid.UUID,
		v2draft.GlareSourceCandidateQuery) (v2draft.GlareSourceCandidatePage, error)
}

func (s *Server) glareDonorRoutes(router chi.Router) {
 s.glareSourceRoutes(router)
	router.Post("/api/v1/tenants/{t}/projects/{id}/glare-mask-pairs", s.maskPairHandler)
	router.Post("/api/v1/tenants/{t}/projects/{id}/glare-source-candidates",
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
			if s.GlareSourceCandidates == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			page, err := s.GlareSourceCandidates.ListProjectSourceCandidates(r.Context(), who,
				tenant, project, query)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			writeJSON(w, http.StatusOK, page)
		})
	router.Post("/api/v1/tenants/{t}/projects/{id}/glare-donor-candidates",
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
			if s.GlareDonorCandidates == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			page, err := s.GlareDonorCandidates.ListProjectCandidates(r.Context(), who,
				tenant, project, query)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			writeJSON(w, http.StatusOK, page)
		})
	router.Post("/api/v1/tenants/{t}/projects/{id}/glare-donor-preparations",
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
			request, err := v2draft.DecodeGlareDonorPreparationRequest(raw)
			if err != nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			if s.GlareDonorPrepare == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			intent, err := s.GlareDonorPrepare.PrepareProjectKeyed(r.Context(), who,
				tenant, project, key, request)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			writeJSON(w, http.StatusOK, intent)
		})
	router.Post("/api/v1/tenants/{t}/projects/{id}/glare-donor-intents",
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
			r.Body = http.MaxBytesReader(w, r.Body, v2draft.MaxGlareDonorIntentBytes)
			var raw json.RawMessage
			if err = decodeJSON(r, &raw); err != nil {
				writeError(w, r, err)
				return
			}
			intent, err := v2draft.DecodeGlareDonorClientIntent(raw)
			if err != nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			if s.GlareDonorSubmit == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			ticket, err := s.GlareDonorSubmit.SubmitProjectKeyed(r.Context(), who,
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
