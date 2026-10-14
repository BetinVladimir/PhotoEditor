package httpapi

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2render"
)

func (s *Server) renderPolicyRoutes(router chi.Router) {
	router.Get("/api/v1/tenants/{t}/projects/{id}/render-policy", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.DB == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		if _, err := (v2render.SQLRenderQualificationResolver{API: s.DB}).Resolve(r.Context(), who, tid, pid); err != nil {
			writeError(w, r, err)
			return
		}
		policy, err := v2render.DefaultRenderPolicy()
		if err != nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		body, err := canonical.Marshal(policy)
		if err != nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write(body)
	})
}
