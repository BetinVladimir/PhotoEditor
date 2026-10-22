package httpapi

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2region"
	"strconv"
)

type AuthoredRegionMediaReader interface {
	Read(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID) (v2region.AuthoredRegionMedia, error)
	Recheck(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, v2region.AuthoredRegionMediaProof) error
}

// RegisterAuthoredRegionMediaRoutes uses the normal server authentication and
// an installed independent physical reader. Nil dependencies fail closed.
func RegisterAuthoredRegionMediaRoutes(r chi.Router, tenantRequest func(*http.Request, bool) (auth.Identity, uuid.UUID, error), reader AuthoredRegionMediaReader) {
	const base = "/api/v1/tenants/{t}/assets/{id}/authored-region-previews/{request}"
	for _, binary := range []bool{false, true} {
		binary := binary
		suffix := "/descriptor"
		if binary {
			suffix = "/derivative"
		}
		r.Get(base+suffix, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if tenantRequest == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			who, tenant, err := tenantRequest(r, true)
			if err != nil {
				writeError(w, r, err)
				return
			}
			asset, err := routeID(r, "id")
			if err != nil {
				writeError(w, r, err)
				return
			}
			request, err := routeID(r, "request")
			if err != nil {
				writeError(w, r, err)
				return
			}
			if reader == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			media, err := reader.Read(r.Context(), who, tenant, asset, request)
			if err == nil {
				media, err = v2region.FreezeAuthoredRegionMedia(media)
			}
			if err == nil && (media.Proof.Tenant != tenant || media.Proof.Asset != asset || media.Proof.Actor != who.UserID || media.Proof.Session != who.SessionID || media.Descriptor.Request != request) {
				err = v2region.ErrRegionInvalid
			}
			if err == nil {
				err = reader.Recheck(r.Context(), who, tenant, asset, request, media.Proof)
			}
			if err == nil {
				err = r.Context().Err()
			}
			if err != nil {
				if errors.Is(err, v2region.ErrRegionInvalid) {
					err = domain.NewError("NOT_FOUND", 404)
				} else {
					err = authoredRegionHTTPError(err)
				}
				writeError(w, r, err)
				return
			}
			if !binary {
				writeJSON(w, 200, media.Descriptor)
				return
			}
			w.Header().Set("Content-Type", media.Descriptor.MediaType)
			w.Header().Set("Content-Length", strconv.Itoa(len(media.Body)))
			w.Header().Set("Content-Disposition", "inline")
			// No redirects, object URLs, range cache, or unverified streaming bytes.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(media.Body)
		})
	}
}
