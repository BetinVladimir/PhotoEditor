package httpapi

import (
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2optical"
)

type PerspectiveDetectedLines interface {
	Describe(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) (v2optical.PerspectiveDetectedLinesProposal, error)
}

func (s *Server) perspectiveDetectedLinesRoutes(router chi.Router) {
	router.Get("/api/v1/tenants/{t}/projects/{id}/versions/{version}/perspective-line-detections/{task}", func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		project, e1 := uuid.Parse(chi.URLParam(r, "id"))
		version, e2 := uuid.Parse(chi.URLParam(r, "version"))
		task, e3 := uuid.Parse(chi.URLParam(r, "task"))
		if e1 != nil || e2 != nil || e3 != nil || project == uuid.Nil || version == uuid.Nil || task == uuid.Nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.PerspectiveDetectedLines == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		proposal, err := s.PerspectiveDetectedLines.Describe(r.Context(), who, tenant, project, version, task)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, proposal)
	})
}
