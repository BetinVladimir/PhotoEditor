package httpapi

import (
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"net/url"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	runservice "photoeditor/backend/internal/run"
	"photoeditor/backend/internal/v2draft"
)

type AuthoredParentService interface {
	BeginAuthoredRender(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, runservice.EditorEffectRequest) (idempotency.Response, error)
	ReadAuthoredParent(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, string) (runservice.AuthoredParentLink, error)
}

// Registration seam is shared by the real Server and transport tests. Production
// always supplies the actual EditorEffectsService, never a candidate projection.
func RegisterAuthoredParentRoutes(r chi.Router, identity func(*http.Request, bool) (auth.Identity, uuid.UUID, error), service AuthoredParentService) {
	const base = "/api/v1/tenants/{t}/projects/{id}/authored-renders"
	r.Post(base, func(w http.ResponseWriter, r *http.Request) {
		who, tenantID, err := identity(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		projectID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !idempotency.ValidKey(key) {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		var body runservice.EditorEffectRequest
		if err = decodeJSON(r, &body); err != nil {
			writeError(w, r, err)
			return
		}
		if service == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		out, err := service.BeginAuthoredRender(r.Context(), who, tenantID, projectID, key, body)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "private, no-store")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
	r.Get(base+"/{parent}", func(w http.ResponseWriter, r *http.Request) {
		who, tenantID, err := identity(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		projectID, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		parent, err := routeID(r, "parent")
		if err != nil {
			writeError(w, r, err)
			return
		}
		query, queryErr := url.ParseQuery(r.URL.RawQuery)
		if queryErr != nil || len(query) != 1 || len(query["request_sha256"]) != 1 {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		expected := query.Get("request_sha256")
		if _, err = domain.ParseDigest(expected); err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if service == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		out, err := service.ReadAuthoredParent(r.Context(), who, tenantID, projectID, parent, expected)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, http.StatusOK, out)
	})
}
func (s *Server) authoredParentRoutes(r chi.Router) {
	drafts := v2draft.Service{DB: s.DB}
	if s.Media != nil {
		drafts.Objects, _ = s.Media.Objects.(v2draft.DraftStore)
		drafts.Bucket = s.Media.Bucket
	}
	service := runservice.EditorEffectsService{Runs: runservice.Service{DB: s.DB}, Drafts: drafts, FreshRenderInstalled: s.AuthoredFinalCalibrations != nil, AuthoredCalibrations: s.AuthoredFinalCalibrations}
	RegisterAuthoredParentRoutes(r, s.tenantRequest, service)
}
