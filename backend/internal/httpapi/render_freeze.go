package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

type RenderFreezer interface {
	Freeze(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, v2render.RenderFreezeRequest) (idempotency.Response, error)
}

func (s *Server) renderFreezeRoutes(router chi.Router) {
	router.Post("/api/v1/tenants/{t}/assets/{id}/v2-draft/freeze", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		asset, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !idempotency.ValidKey(key) {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, v2render.MaxRenderFreezeBytes)
		var raw json.RawMessage
		if err := decodeJSON(r, &raw); err != nil {
			writeError(w, r, err)
			return
		}
		request, err := v2render.DecodeRenderFreezeRequest(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		expected, err := v2render.RenderFreezeDocumentID(tid, who.UserID, asset, key, request)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		freezer := s.RenderFreezer
		if freezer == nil {
			drafts := v2draft.Service{DB: s.DB}
			if s.Media != nil {
				drafts.Objects, _ = s.Media.Objects.(v2draft.DraftStore)
				drafts.Bucket = s.Media.Bucket
			}
			freezer = v2render.SQLRenderFreezeService{Drafts: drafts, Qualification: v2render.SQLRenderQualificationResolver{API: s.DB}}
		}
		out, err := freezer.Freeze(r.Context(), who, tid, asset, key, request)
		if err != nil {
			switch {
			case errors.Is(err, v2input.ErrInvalid):
				err = domain.NewError("VALIDATION_FAILED", 422)
			case errors.Is(err, v2input.ErrConflict):
				err = domain.NewError("IDEMPOTENCY_CONFLICT", 409)
			case errors.Is(err, v2input.ErrExpired):
				err = domain.NewError("SNAPSHOT_EXPIRED", 410)
			case errors.Is(err, objectstore.ErrUnavailable), errors.Is(err, objectstore.ErrMissing), errors.Is(err, objectstore.ErrIntegrity):
				err = domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
			}
			writeError(w, r, err)
			return
		}
		if _, err := v2render.DecodeRenderFreezeResult(out.Body, expected, request); err != nil || out.Status != 200 || out.ResourceID == nil || *out.ResourceID != expected {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, http.StatusOK, out.Body)
	})
}
