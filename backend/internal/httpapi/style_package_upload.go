package httpapi

import (
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/styleupload"
)

// Package publication is separate from photo upload/ingest and Style acceptance.
func (s *Server) stylePackageUploadRoutes(router chi.Router) {
	root := "/api/v1/tenants/{t}/projects/{project}/style-package-uploads"
	for _, action := range []string{"begin", "status", "parts", "complete", "abort"} {
		action := action
		handler := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			who, tid, e := s.tenantRequest(r, action != "status")
			if e != nil {
				writeError(w, r, e)
				return
			}
			pid, e := uuid.Parse(chi.URLParam(r, "project"))
			if e != nil || pid == uuid.Nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			var id uuid.UUID
			if action != "begin" {
				id, e = uuid.Parse(chi.URLParam(r, "upload"))
				if e != nil || id == uuid.Nil {
					writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
					return
				}
			}
			if s.Media == nil {
				writeError(w, r, domain.NewError("SERVICE_UNAVAILABLE", 503))
				return
			}
			objects, ok := s.Media.Objects.(styleupload.Objects)
			if !ok {
				writeError(w, r, domain.NewError("SERVICE_UNAVAILABLE", 503))
				return
			}
			service := styleupload.Service{DB: s.DB, Objects: objects, Bucket: s.Media.Bucket}
			var out any
			switch action {
			case "begin":
				var request styleupload.BeginRequest
				e = decodeJSON(r, &request)
				if e == nil {
					out, e = service.Begin(r.Context(), who, tid, pid, r.Header.Get("Idempotency-Key"), request)
				}
			case "status":
				out, e = service.Get(r.Context(), who, tid, pid, id)
			case "parts":
				var request struct {
					Parts []int `json:"part_numbers"`
				}
				e = decodeJSON(r, &request)
				if e == nil {
					var parts any
					parts, e = service.Sign(r.Context(), who, tid, pid, id, request.Parts)
					out = map[string]any{"upload_id": id, "parts": parts}
				}
			case "complete":
				var request styleupload.CompleteRequest
				e = decodeJSON(r, &request)
				if e == nil {
					out, e = service.Complete(r.Context(), who, tid, pid, id, r.Header.Get("Idempotency-Key"), request)
				}
			case "abort":
				out, e = service.Abort(r.Context(), who, tid, pid, id)
			}
			if e != nil {
				writeError(w, r, e)
				return
			}
			writeJSON(w, http.StatusOK, out)
		}
		switch action {
		case "begin":
			router.Post(root, handler)
		case "status":
			router.Get(root+"/{upload}", handler)
		default:
			router.Post(root+"/{upload}/"+action, handler)
		}
	}
}
