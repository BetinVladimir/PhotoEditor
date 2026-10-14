package httpapi

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2render"
)

func (s *Server) perspectiveProposalRoutes(router chi.Router) {
 s.perspectiveReplacementReviewRoutes(router)
	router.Get("/api/v1/tenants/{t}/projects/{id}/versions/{version}/perspective-proposals/{task}", func(w http.ResponseWriter, r *http.Request) {
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
		if s.PerspectiveProposals == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		proposal, err := s.PerspectiveProposals.Describe(r.Context(), who, tenant, project, version, task)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, proposal)
	})
	router.Post("/api/v1/tenants/{t}/projects/{id}/versions/{version}/perspective-proposals/{task}/apply", func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, true)
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
		if s.PerspectiveProposals == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		var raw json.RawMessage
		if err = decodeJSON(r, &raw); err != nil {
			writeError(w, r, err)
			return
		}
		decision, err := v2render.DecodePerspectiveApplyDecision(raw)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		proposal, err := s.PerspectiveProposals.Apply(r.Context(), who, tenant, project, version, task, decision)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, proposal)
	})
	router.Get("/api/v1/tenants/{t}/projects/{id}/versions/{version}/perspective-proposals/{task}/quality", func(w http.ResponseWriter, r *http.Request) {
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
		if s.PerspectiveProposals == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		proposal, err := s.PerspectiveProposals.QualityReview(r.Context(), who, tenant, project, version, task)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, proposal)
	})
	router.Get("/api/v1/tenants/{t}/projects/{id}/versions/{version}/perspective-proposals/{task}/previews/{variant}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		who, tenant, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		project, e1 := uuid.Parse(chi.URLParam(r, "id"))
		version, e2 := uuid.Parse(chi.URLParam(r, "version"))
		task, e3 := uuid.Parse(chi.URLParam(r, "task"))
		variant := chi.URLParam(r, "variant")
		if e1 != nil || e2 != nil || e3 != nil || project == uuid.Nil || version == uuid.Nil || task == uuid.Nil || (variant != "preview1600" && variant != "preview2560") {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.PerspectiveProposals == nil || s.PerspectiveProposals.PreviewSigner == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		preview, err := s.PerspectiveProposals.Preview(r.Context(), who, tenant, project, version, task, variant)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		writeJSON(w, 200, preview)
	})

}
