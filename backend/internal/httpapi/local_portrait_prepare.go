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

type LocalPortraitPrepare interface {
	PrepareProjectKeyed(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string,
		v2draft.LocalPortraitClientSelection) (v2draft.LocalPortraitClientSelection, error)
}

type LocalPortraitSubmit interface {
	SubmitProjectKeyed(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string,
		v2draft.LocalPortraitClientSelection) (v2draft.LocalPortraitTicket, error)
}

type LocalPortraitService interface {
	LocalPortraitPrepare
	LocalPortraitSubmit
	GlareMaskPairs
}

func (s *Server) localPortraitRoutes(router chi.Router) {
	router.Post("/api/v1/tenants/{t}/projects/{id}/local-portrait-mask-pairs", s.reflectionMaskPairHandler)
	router.Post("/api/v1/tenants/{t}/projects/{id}/local-portrait-preparations", func(w http.ResponseWriter, r *http.Request) {
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
		selection, err := v2draft.DecodeLocalPortraitClientSelection(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.LocalPortraitPrepare == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		prepared, err := s.LocalPortraitPrepare.PrepareProjectKeyed(r.Context(), who, tenant, project, key, selection)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		writeJSON(w, http.StatusOK, prepared)
	})
	router.Post("/api/v1/tenants/{t}/projects/{id}/local-portrait-intents", func(w http.ResponseWriter, r *http.Request) {
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
		selection, err := v2draft.DecodeLocalPortraitClientSelection(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.LocalPortraitSubmit == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		ticket, err := s.LocalPortraitSubmit.SubmitProjectKeyed(r.Context(), who, tenant, project, key, selection)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		writeJSON(w, http.StatusAccepted, ticket)
	})
}
