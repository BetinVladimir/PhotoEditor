package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/portal"
)

func (s *Server) portalRoutes(r chi.Router) {
	r.Post("/api/v1/portal/sessions", func(w http.ResponseWriter, r *http.Request) {
		if err := s.bootstrapGuard(r); err != nil {
			writeError(w, r, err)
			return
		}
		var request portal.ExchangeRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.Exchange(r.Context(), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		maxAge := int(time.Until(out.ExpiresAt).Seconds())
		if maxAge < 1 {
			writeError(w, r, domain.NewError("INVALID_SHARE", 401))
			return
		}
		s.cookie(w, "portal_session", out.Token, out.ExpiresAt, maxAge)
		writeJSON(w, http.StatusCreated, out)
	})
	r.Get("/api/v1/portal/collection", func(w http.ResponseWriter, r *http.Request) {
		token, err := s.portalToken(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.Collection(r.Context(), token)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", strconv.Quote(out.Revision))
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/portal/items", func(w http.ResponseWriter, r *http.Request) {
		token, err := s.portalToken(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		pageSize, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var sectionID *uuid.UUID
		if raw := r.URL.Query().Get("section_id"); raw != "" {
			id, parseErr := uuid.Parse(raw)
			if parseErr != nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			sectionID = &id
		}
		out, err := s.Portal.Items(r.Context(), token, sectionID, pageSize, r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", strconv.Quote(out.Revision))
		writeJSON(w, http.StatusOK, out)
	})
	r.Get("/api/v1/portal/events", func(w http.ResponseWriter, r *http.Request) {
		token, err := s.portalToken(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		n, err := limit(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.Events(r.Context(), token, r.URL.Query().Get("after"), n)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/portal/media-links", func(w http.ResponseWriter, r *http.Request) {
		token, csrf, err := s.portalMutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request portal.MediaRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.MediaLinks(r.Context(), token, csrf, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/portal/selection-snapshots", func(w http.ResponseWriter, r *http.Request) {
		token, csrf, err := s.portalMutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request portal.SelectionRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.CreateSelection(r.Context(), token, csrf, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, out)
	})
	r.Post("/api/v1/portal/download-jobs", func(w http.ResponseWriter, r *http.Request) {
		token, csrf, err := s.portalMutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request portal.DownloadRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.StartDownload(r.Context(), token, csrf, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, out)
	})
	r.Get("/api/v1/portal/download-jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		token, err := s.portalToken(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		out, err := s.Portal.Download(r.Context(), token, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/portal/download-jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		token, csrf, err := s.portalMutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		out, err := s.Portal.CancelDownload(r.Context(), token, csrf, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/portal/download-jobs/{id}/download-intent", func(w http.ResponseWriter, r *http.Request) {
		token, csrf, err := s.portalMutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		var request portal.DownloadIntentRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.CreateDownloadIntent(r.Context(), token, csrf, r.Header.Get("Idempotency-Key"), id, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, out)
	})
	r.Post("/api/v1/portal/items/{id}/download-intent", func(w http.ResponseWriter, r *http.Request) {
		token, csrf, err := s.portalMutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		var request portal.SingleDownloadRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.CreateItemDownloadIntent(r.Context(), token, csrf, r.Header.Get("Idempotency-Key"), id, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, out)
	})
	r.Post("/api/v1/downloads/redeem", func(w http.ResponseWriter, r *http.Request) {
		var request portal.DownloadRedeemRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.RedeemDownloadIntent(r.Context(), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/portal/uploads", func(w http.ResponseWriter, r *http.Request) {
		token, csrf, err := s.portalMutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request portal.GuestUploadBegin
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.BeginUpload(r.Context(), token, csrf, r.Header.Get("Idempotency-Key"), request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", strconv.Quote(out.Revision))
		writeJSON(w, http.StatusCreated, out)
	})
	r.Get("/api/v1/portal/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		token, err := s.portalToken(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.GetUpload(r.Context(), token, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", strconv.Quote(out.Revision))
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/portal/uploads/{id}/parts", func(w http.ResponseWriter, r *http.Request) {
		token, csrf, err := s.portalMutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request portal.GuestPartRequest
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.SignUploadParts(r.Context(), token, csrf, id, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/portal/uploads/{id}/abort", func(w http.ResponseWriter, r *http.Request) {
		token, csrf, err := s.portalMutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.AbortUpload(r.Context(), token, csrf, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", strconv.Quote(out.Revision))
		writeJSON(w, http.StatusOK, out)
	})
	r.Post("/api/v1/portal/uploads/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		token, csrf, err := s.portalMutation(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		id, err := routeID(r, "id")
		if err != nil {
			writeError(w, r, err)
			return
		}
		var request portal.GuestComplete
		if err = decodeJSON(r, &request); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := s.Portal.CompleteUpload(r.Context(), token, csrf, r.Header.Get("Idempotency-Key"), id, request)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, out)
	})
}

func (s *Server) portalToken(r *http.Request) (string, error) {
	cookie, err := r.Cookie(s.cookieName("portal_session"))
	if err != nil || cookie.Value == "" {
		return "", domain.NewError("INVALID_GUEST_SESSION", 401)
	}
	return cookie.Value, nil
}

func (s *Server) portalMutation(r *http.Request) (string, string, error) {
	if err := s.checkOrigin(r); err != nil {
		return "", "", err
	}
	token, err := s.portalToken(r)
	if err != nil {
		return "", "", err
	}
	csrf := r.Header.Get("X-CSRF-Token")
	if csrf == "" {
		return "", "", domain.NewError("CSRF_INVALID", 403)
	}
	return token, csrf, nil
}
