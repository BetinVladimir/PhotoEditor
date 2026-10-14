package httpapi

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2render"
)

type MultiProposalReader interface {
	Describe(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, string) (v2render.MultiProposal, error)
}

type MultiPreviewReader interface {
	Preview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, string, string) (v2render.MultiPreview, error)
}

type MultiQualityReader interface {
	QualityReview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, string) (v2render.MultiQualityReview, error)
}

type MultiApplyWriter interface {
	Apply(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, string, v2render.MultiApplyDecision) (v2render.MultiApplied, error)
}

func (s *Server) multiProposalRoutes(router chi.Router) {
	for _, route := range []struct{ path, operation string }{{"panorama-proposals", "optical.panorama"}, {"focus-stack-proposals", "optical.focus_stack"}} {
		router.Post("/api/v1/tenants/{t}/projects/{id}/"+route.path+"/{task}/apply", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			who, tenant, err := s.tenantRequest(r, true)
			if err != nil {
				writeError(w, r, err)
				return
			}
			project, e1 := uuid.Parse(chi.URLParam(r, "id"))
			task, e2 := uuid.Parse(chi.URLParam(r, "task"))
			if e1 != nil || e2 != nil || project == uuid.Nil || task == uuid.Nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			writer, ok := s.MultiProposals.(MultiApplyWriter)
			if !ok {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			var raw json.RawMessage
			if err = decodeJSON(r, &raw); err != nil {
				writeError(w, r, err)
				return
			}
			decision, err := v2render.DecodeMultiApplyDecision(raw)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			value, err := writer.Apply(r.Context(), who, tenant, project, task, route.operation, decision)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			writeJSON(w, 200, value)
		})

		router.Get("/api/v1/tenants/{t}/projects/{id}/"+route.path+"/{task}", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			who, tenant, err := s.tenantRequest(r, false)
			if err != nil {
				writeError(w, r, err)
				return
			}
			project, e1 := uuid.Parse(chi.URLParam(r, "id"))
			task, e2 := uuid.Parse(chi.URLParam(r, "task"))
			if e1 != nil || e2 != nil || project == uuid.Nil || task == uuid.Nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			if s.MultiProposals == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			proposal, err := s.MultiProposals.Describe(r.Context(), who, tenant, project, task, route.operation)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			writeJSON(w, 200, proposal)
		})
		router.Get("/api/v1/tenants/{t}/projects/{id}/"+route.path+"/{task}/quality", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			who, tenant, err := s.tenantRequest(r, false)
			if err != nil {
				writeError(w, r, err)
				return
			}
			project, e1 := uuid.Parse(chi.URLParam(r, "id"))
			task, e2 := uuid.Parse(chi.URLParam(r, "task"))
			if e1 != nil || e2 != nil || project == uuid.Nil || task == uuid.Nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			reader, ok := s.MultiProposals.(MultiQualityReader)
			if !ok {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			value, err := reader.QualityReview(r.Context(), who, tenant, project, task, route.operation)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			writeJSON(w, 200, value)
		})

		router.Get("/api/v1/tenants/{t}/projects/{id}/"+route.path+"/{task}/previews/{variant}", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			who, tenant, err := s.tenantRequest(r, false)
			if err != nil {
				writeError(w, r, err)
				return
			}
			project, e1 := uuid.Parse(chi.URLParam(r, "id"))
			task, e2 := uuid.Parse(chi.URLParam(r, "task"))
			variant := chi.URLParam(r, "variant")
			if e1 != nil || e2 != nil || project == uuid.Nil || task == uuid.Nil || (variant != "preview1600" && variant != "preview2560") {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			reader, ok := s.MultiProposals.(MultiPreviewReader)
			if !ok {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			preview, err := reader.Preview(r.Context(), who, tenant, project, task, route.operation, variant)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			writeJSON(w, 200, preview)
		})

	}
}
