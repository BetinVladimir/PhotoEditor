package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"net/http"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

func (s *Server) renderInputSnapshotRoutes(router chi.Router) {
	router.Post("/api/v1/tenants/{t}/projects/{id}/render-input-snapshots", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pid, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !idempotency.ValidKey(key) {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, v2render.MaxRenderSubmissionBytes)
		var raw json.RawMessage
		if err := decodeJSON(r, &raw); err != nil {
			writeError(w, r, err)
			return
		}
		sources, err := v2render.DecodeRenderInputSnapshotRequest(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.DB == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		out, err := (v2render.SQLRenderInputSnapshotService{DB: s.DB}).Create(r.Context(), who, tid, pid, key, sources)
		if err != nil {
			if errors.Is(err, v2input.ErrInvalid) {
				err = domain.NewError("VALIDATION_FAILED", 422)
			}
			writeError(w, r, err)
			return
		}
		id, _ := v2render.RenderInputSnapshotID(tid, who.UserID, pid, key)
		digest, _ := canonical.Hash(sources)
		_, decodeErr := v2render.DecodeRenderInputSnapshotResult(out.Body, id, digest, len(sources))
		if decodeErr != nil || out.Status != 201 || out.ResourceID == nil || *out.ResourceID != id {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		if out.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		w.Write(out.Body)
	})
}
