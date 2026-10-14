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

type RelightCandidates interface {
	ListProjectCandidates(context.Context, auth.Identity, uuid.UUID, uuid.UUID, v2portrait.RelightCandidateQuery) (v2portrait.RelightCandidatePage, error)
}

func (s *Server) relightCandidateRoutes(router chi.Router) {
	// POST carries an exact source binding; discovery does not create resources.
	router.Post("/api/v1/tenants/{t}/projects/{id}/relight-candidates", func(w http.ResponseWriter, r *http.Request) {
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
		query, err := v2portrait.DecodeRelightCandidateQuery(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		service, ok := s.RelightSubmit.(RelightCandidates)
		if !ok {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		page, err := service.ListProjectCandidates(r.Context(), who, tenant, project, query)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		writeJSON(w, 200, page)
	})
}
