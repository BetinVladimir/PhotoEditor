package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2region"
)

type RegionQueue interface {
	Queue(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, v2region.RegionPreviewRequest) (v2input.JobTicket, error)
}

func (s *Server) regionRoutes(r chi.Router) {
	r.Get("/api/v1/tenants/{t}/assets/{id}/region-previews/{run}/descriptor", func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		asset, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		run, err := routeID(r, "run")
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.Media == nil || s.DB == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		objects, ok := s.Media.Objects.(v2region.RegionObjectReader)
		if !ok {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		service := v2region.Service{Objects: objects, Bucket: s.Media.Bucket}
		descriptor, err := service.ReadDescriptorScoped(r.Context(), s.DB, tenant, asset, run, who.UserID, who.SessionID, s.Media.SRGBSHA256)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, v2region.ErrRegionInvalid) {
				err = domain.NewError("NOT_FOUND", 404)
			}
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, descriptor)
	})

	r.Post("/api/v1/tenants/{t}/assets/{id}/region-previews", func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, true)
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
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		var raw json.RawMessage
		if err = decodeJSON(r, &raw); err != nil {
			writeError(w, r, err)
			return
		}
		request, err := v2region.DecodeRegionPreviewRequest(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.RegionQueue == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		ticket, err := s.RegionQueue.Queue(r.Context(), who, tenant, asset, key, request)
		if err != nil {
			switch {
			case errors.Is(err, v2input.ErrConflict):
				err = domain.NewError("IDEMPOTENCY_CONFLICT", 409)
			case errors.Is(err, v2input.ErrExpired):
				err = domain.NewError("SNAPSHOT_EXPIRED", 410)
			case errors.Is(err, v2region.ErrRegionUnavailable):
				err = domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
			case errors.Is(err, v2region.ErrRegionInvalid):
				err = domain.NewError("VALIDATION_FAILED", 422)
			}
			writeError(w, r, err)
			return
		}
		if ticket.RunID == uuid.Nil || ticket.WorkflowID == uuid.Nil || ticket.State != "queued" {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		writeJSON(w, 202, ticket)
	})
}
