package httpapi

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2portrait"
)

type RelightPreparation interface {
	PrepareProjectIntent(context.Context, auth.Identity, uuid.UUID, uuid.UUID, v2portrait.RelightPreparationRequest) ([]byte, error)
}

func (s *Server) relightPreparationRoutes(router chi.Router) {
	router.Post("/api/v1/tenants/{t}/projects/{id}/relight-preparation", func(w http.ResponseWriter, r *http.Request) {
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
		request, err := v2portrait.DecodeRelightPreparationRequest(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		service, ok := s.RelightSubmit.(RelightPreparation)
		if !ok {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		body, err := service.PrepareProjectIntent(r.Context(), who, tenant, project, request)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		if _, err = v2portrait.DecodeRelightDraftIntent(body); err != nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		writeJSON(w, 200, json.RawMessage(body))
	})
}
