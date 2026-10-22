package httpapi

import (
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"io"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/semanticreview"
)

type SemanticShownVersion interface {
	Materialize(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (semanticreview.ShownVersionResult, error)
}

var _ SemanticShownVersion = (*semanticreview.ShownVersionService)(nil)

func (s *Server) semanticShownVersionRoutes(router chi.Router) {
	router.Post("/api/v1/tenants/{t}/semantic-targets/{target}/version", func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		target, err := routeID(r, "target")
		if err != nil {
			writeError(w, r, err)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 || r.URL.RawQuery != "" {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		if s.SemanticShownVersion == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		result, err := s.SemanticShownVersion.Materialize(r.Context(), who, tenant, target)
		if r.Context().Err() != nil {
			return
		}
		if err != nil {
			writeError(w, r, semanticReviewError(err))
			return
		}
		if result.AssetVersionID == uuid.Nil || result.AssetID == uuid.Nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		writeJSON(w, 200, result)
	})
}
