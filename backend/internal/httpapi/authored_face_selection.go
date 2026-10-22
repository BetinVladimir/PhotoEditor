package httpapi

import (
	"errors"
	"net/http"
 "net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

type authoredFaceDecisionBody struct {
	Revision int         `json:"revision"`
	Decision string      `json:"decision"`
	Selected []uuid.UUID `json:"selected_objects"`
}

func authoredFaceSelectionHTTPError(err error) error {
	switch {
	case errors.Is(err, v2input.ErrInvalid):
		return domain.NewError("VALIDATION_FAILED", 422)
	case errors.Is(err, v2input.ErrConflict):
		return domain.NewError("AUTHORED_FACE_SELECTION_CONFLICT", 409)
	case errors.Is(err, v2input.ErrExpired):
		return domain.NewError("AUTHORED_FACE_SELECTION_STALE", 410)
	default:
		return err
	}
}
func authoredFaceSelectionScope(r *http.Request) (uuid.UUID, int, string, error) {
	parent, err := routeID(r, "parent")
	if err != nil {
		return uuid.Nil, 0, "", err
	}
	ordinal, err := strconv.Atoi(chi.URLParam(r, "ordinal"))
	if err != nil || ordinal < 1 || ordinal > 4998 {
		return uuid.Nil, 0, "", domain.NewError("VALIDATION_FAILED", 422)
	}
	sha := chi.URLParam(r, "candidate")
	if len(sha) != 64 {
		return uuid.Nil, 0, "", domain.NewError("VALIDATION_FAILED", 422)
	}
	for _, c := range sha {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return uuid.Nil, 0, "", domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	return parent, ordinal, sha, nil
}

// Mount under the same authenticated route group as authored region requests.
// No client-supplied user/project/source is accepted. Tenant.WithTenant in the
// real service repeats membership and the SQL current-source/decision authority.
func (s *Server) authoredFaceSelectionRoutes(r chi.Router) {

	r.Get("/api/v1/tenants/{t}/authored-renders/{parent}/face-selections", func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		parent, err := routeID(r, "parent")
		if err != nil {
			writeError(w, r, err)
			return
		}
		after, limit, err := authoredFaceSelectionPageQuery(r.URL.RawQuery)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.DB == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		page, err := (v2render.AuthoredFaceSelectionService{API: s.DB}).Discover(r.Context(), who, tenant, parent, after, limit)
		if err != nil {
			writeError(w, r, authoredFaceSelectionHTTPError(err))
			return
		}
		writeJSON(w, 200, page)
	})
	const base = "/api/v1/tenants/{t}/authored-renders/{parent}/phases/{ordinal}/face-selection/{candidate}"
	r.Get(base, func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		parent, ordinal, sha, err := authoredFaceSelectionScope(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s.DB == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		view, err := (v2render.AuthoredFaceSelectionService{API: s.DB}).Read(r.Context(), who, tenant, parent, ordinal, sha)
		if err != nil {
			writeError(w, r, authoredFaceSelectionHTTPError(err))
			return
		}
		writeJSON(w, 200, view)
	})
	r.Post(base+"/decision", func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		parent, ordinal, sha, err := authoredFaceSelectionScope(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !idempotency.ValidKey(key) {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		var body authoredFaceDecisionBody
		if err = decodeJSON(r, &body); err != nil {
			writeError(w, r, err)
			return
		}
		if s.DB == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		view, err := (v2render.AuthoredFaceSelectionService{API: s.DB}).Decide(r.Context(), who, tenant, parent, ordinal, body.Revision, sha, key, body.Decision, body.Selected)
		if err != nil {
			writeError(w, r, authoredFaceSelectionHTTPError(err))
			return
		}
		writeJSON(w, 200, view)
	})
}

// Parse a closed, unambiguous page scope before consulting any database.
func authoredFaceSelectionPageQuery(raw string) (int, int, error) {
 invalid := func() (int, int, error) { return 0, 0, domain.NewError("VALIDATION_FAILED", 422) }
 query, err := url.ParseQuery(raw)
 if err != nil { return invalid() }
 after, limit := 0, 16
 for key, values := range query {
  if len(values) != 1 || values[0] == "" { return invalid() }
  value, err := strconv.Atoi(values[0])
  if err != nil { return invalid() }
  switch key {
  case "after_ordinal":
   if value < 0 || value > 4998 { return invalid() }
   after = value
  case "limit":
   if value < 1 || value > 16 { return invalid() }
   limit = value
  default: return invalid()
  }
 }
 return after, limit, nil
}
