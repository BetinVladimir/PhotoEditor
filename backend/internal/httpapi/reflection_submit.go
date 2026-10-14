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

type ReflectionPrepare interface {
	PrepareProjectKeyed(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string,
		v2draft.ReflectionPreparationRequest) (v2draft.ReflectionClientIntent, error)
}

type ReflectionSubmit interface {
	SubmitProjectKeyed(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string,
		v2draft.ReflectionClientIntent) (v2draft.RestorationAdmissionTicket, error)
}

type ReflectionService interface {
	ReflectionPrepare
	ReflectionSubmit
	GlareMaskPairs
}

func (s *Server) reflectionRoutes(router chi.Router) {
	router.Post("/api/v1/tenants/{t}/projects/{id}/reflection-mask-pairs", s.reflectionMaskPairHandler)
	router.Post("/api/v1/tenants/{t}/projects/{id}/reflection-preparations",
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
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			var raw json.RawMessage
			if err := decodeJSON(r, &raw); err != nil {
				writeError(w, r, err)
				return
			}
			request, err := v2draft.DecodeReflectionPreparationRequest(raw)
			if err != nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			if s.ReflectionPrepare == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			intent, err := s.ReflectionPrepare.PrepareProjectKeyed(r.Context(), who,
				tenant, project, key, request)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			writeJSON(w, http.StatusOK, intent)
		})
	router.Post("/api/v1/tenants/{t}/projects/{id}/reflection-intents",
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
			if err := decodeJSON(r, &raw); err != nil {
				writeError(w, r, err)
				return
			}
			intent, err := v2draft.DecodeReflectionClientIntent(raw)
			if err != nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			if s.ReflectionSubmit == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			ticket, err := s.ReflectionSubmit.SubmitProjectKeyed(r.Context(), who,
				tenant, project, key, intent)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			writeJSON(w, http.StatusAccepted, ticket)
		})
}
