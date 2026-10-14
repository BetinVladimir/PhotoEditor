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
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"strings"
)

type RenderQueue interface {
	Queue(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, v2render.RenderBatchSubmissionRequest) (v2render.RenderBatchTicket, error)
}

// Routing middleware runs before chi has populated path parameters.
func isRenderSubmissionPath(r *http.Request) bool {
	parts := strings.Split(r.URL.Path, "/")
	return r.Method == http.MethodPost && len(parts) == 8 && parts[1] == "api" && parts[2] == "v1" && parts[3] == "tenants" && parts[4] != "" && parts[5] == "projects" && parts[6] != "" && (parts[7] == "render-batches" || parts[7] == "render-input-snapshots")
}

func (s *Server) renderRoutes(router chi.Router) {
	s.renderInputSnapshotRoutes(router)
	s.renderPolicyRoutes(router)
	router.Post("/api/v1/tenants/{t}/projects/{id}/render-batches", func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		project, err := routeID(r, "id")
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
		request, err := v2render.DecodeRenderBatchSubmissionRequest(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		batch, _, err := v2render.CompileRenderSubmissionIdentity(tenant, who.UserID, project, key, request)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.RenderQueue == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		ticket, err := s.RenderQueue.Queue(r.Context(), who, tenant, project, key, request)
		if err != nil {
			switch {
			case errors.Is(err, v2input.ErrConflict):
				err = domain.NewError("IDEMPOTENCY_CONFLICT", 409)
			case errors.Is(err, v2input.ErrExpired):
				err = domain.NewError("SNAPSHOT_EXPIRED", 410)
			case errors.Is(err, v2input.ErrInvalid):
				err = domain.NewError("VALIDATION_FAILED", 422)
			}
			writeError(w, r, err)
			return
		}
		if v2render.ValidateRenderBatchTicket(batch, ticket) != nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		writeJSON(w, http.StatusAccepted, ticket)
	})
}
