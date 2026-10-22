package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

type ExportSigner interface {
	SignExport(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, string) (v2input.JobTicket, error)
}
type SigningPolicyLister interface {
	AvailableSigningPolicies(context.Context, auth.Identity, uuid.UUID, uuid.UUID) ([]provenance.SigningPolicyChoice, error)
}
type DeliverySigningPolicyLister interface {
	AvailableDeliverySigningPolicies(context.Context, auth.Identity, uuid.UUID, uuid.UUID, []string) ([]provenance.DeliverySigningPolicyChoice, error)
}

func (s *Server) provenanceRoutes(router chi.Router) {
	router.Get("/api/v1/tenants/{t}/projects/{project}/delivery-signing-policies", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		project, err := uuid.Parse(chi.URLParam(r, "project"))
		if err != nil || project == uuid.Nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		mimes := query["mime"]
		if err != nil || len(query) != 1 || len(mimes) < 1 || len(mimes) > 2 {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		seen := map[string]bool{}
		for _, mime := range mimes {
			if (mime != "image/jpeg" && mime != "image/png") || seen[mime] {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			seen[mime] = true
		}
		lister, ok := s.ExportSigner.(DeliverySigningPolicyLister)
		if !ok {
			writeError(w, r, domain.NewError("SIGNING_UNAVAILABLE", 503))
			return
		}
		serveDeliverySigningPolicies(w, r, who, tid, project, mimes, lister)
	})
	router.Get("/api/v1/tenants/{t}/exports/{id}/signing-policies", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, false)
		if err != nil {
			writeError(w, r, err)
			return
		}
		export, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil || export == uuid.Nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		lister, ok := s.ExportSigner.(SigningPolicyLister)
		if !ok {
			writeError(w, r, domain.NewError("SIGNING_UNAVAILABLE", 503))
			return
		}
		serveSigningPolicies(w, r, who, tid, export, lister)
	})
	router.Post("/api/v1/tenants/{t}/exports/{id}/provenance", func(w http.ResponseWriter, r *http.Request) {
		who, tid, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		export, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil || export == uuid.Nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		serveSignExport(w, r, who, tid, export, s.ExportSigner)
	})
}

func serveDeliverySigningPolicies(w http.ResponseWriter, r *http.Request, who auth.Identity, tenant, project uuid.UUID, mimes []string, lister DeliverySigningPolicyLister) {
	if lister == nil || tenant == uuid.Nil || project == uuid.Nil {
		writeError(w, r, domain.NewError("SIGNING_UNAVAILABLE", 503))
		return
	}
	if err := r.Context().Err(); err != nil {
		writeError(w, r, err)
		return
	}
	choices, err := lister.AvailableDeliverySigningPolicies(r.Context(), who, tenant, project, append([]string(nil), mimes...))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := r.Context().Err(); err != nil {
		writeError(w, r, err)
		return
	}
	if len(choices) > provenance.MaxInstalledSigningPolicies {
		writeError(w, r, domain.NewError("SIGNING_UNAVAILABLE", 503))
		return
	}
	owned := make([]provenance.DeliverySigningPolicyChoice, len(choices))
	seen := map[uuid.UUID]bool{}
	for index, choice := range choices {
		revision, parseErr := strconv.ParseUint(choice.Revision, 10, 63)
		validSHA := len(choice.SHA256) == 64
		for _, digit := range choice.SHA256 {
			if digit < '0' || digit > '9' && (digit < 'a' || digit > 'f') {
				validSHA = false
				break
			}
		}
		if choice.ID == uuid.Nil || seen[choice.ID] || !validSHA || parseErr != nil || revision == 0 || strconv.FormatUint(revision, 10) != choice.Revision || index > 0 && owned[index-1].ID.String() >= choice.ID.String() {
			writeError(w, r, domain.NewError("SIGNING_UNAVAILABLE", 503))
			return
		}
		seen[choice.ID] = true
		owned[index] = choice
	}
	writeJSON(w, 200, struct {
		Items []provenance.DeliverySigningPolicyChoice `json:"items"`
	}{owned})
}

func serveSignExport(w http.ResponseWriter, r *http.Request, who auth.Identity, tid, export uuid.UUID, signer ExportSigner) {
	if err := r.Context().Err(); err != nil { writeError(w,r,err); return }
	key := r.Header.Get("Idempotency-Key")
	if !idempotency.ValidKey(key) || tid == uuid.Nil || export == uuid.Nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var request struct {
		Policy uuid.UUID `json:"signing_policy_id"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, r, err)
		return
	}
	if request.Policy == uuid.Nil {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	if signer == nil {
		writeError(w, r, domain.NewError("SIGNING_UNAVAILABLE", 503))
		return
	}
	if err := r.Context().Err(); err != nil { writeError(w,r,err); return }
	ticket, err := signer.SignExport(r.Context(), who, tid, export, request.Policy, key)
	if err != nil {
		switch {
		case errors.Is(err, provenance.ErrInvalid):
			err = domain.NewError("VALIDATION_FAILED", 422)
		case errors.Is(err, artifactjson.ErrConflict):
			err = domain.NewError("IDEMPOTENCY_CONFLICT", 409)
		case errors.Is(err, artifactjson.ErrStale):
			err = domain.NewError("SNAPSHOT_EXPIRED", 410)
		case errors.Is(err, artifactjson.ErrQuota):
			err = domain.NewError("QUOTA_EXCEEDED", 409)
		}
		writeError(w, r, err)
		return
	}
	if err := r.Context().Err(); err != nil { writeError(w,r,err); return }
	workflow := uuid.NewSHA1(tid, []byte("provenance:"+who.UserID.String()+":"+key))
	if ticket.WorkflowID != workflow || ticket.RunID != uuid.NewSHA1(workflow, []byte("run")) || ticket.State != "queued" {
		writeError(w, r, domain.NewError("SIGNING_UNAVAILABLE", 503))
		return
	}
	writeJSON(w, http.StatusAccepted, ticket)
}

// Preserve the same bounded catalogue contract as the isolated worker/client.
// A provider configuration error must not return200 with an undecodable body.
func serveSigningPolicies(w http.ResponseWriter, r *http.Request, who auth.Identity, tid, export uuid.UUID, lister SigningPolicyLister) {
	if lister == nil {
		writeError(w, r, domain.NewError("SIGNING_UNAVAILABLE", 503))
		return
	}
	if err := r.Context().Err(); err != nil {
		writeError(w, r, err)
		return
	}
	choices, err := lister.AvailableSigningPolicies(r.Context(), who, tid, export)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := r.Context().Err(); err != nil {
		writeError(w, r, err)
		return
	}
	if len(choices) > provenance.MaxInstalledSigningPolicies {
		writeError(w, r, domain.NewError("SIGNING_UNAVAILABLE", 503))
		return
	}
	owned := make([]provenance.SigningPolicyChoice, len(choices))
	seen := make(map[uuid.UUID]bool, len(choices))
	for i, choice := range choices {
		revision, err := strconv.ParseUint(choice.Revision, 10, 63)
		if choice.ID == uuid.Nil || seen[choice.ID] || err != nil || revision == 0 || strconv.FormatUint(revision, 10) != choice.Revision {
			writeError(w, r, domain.NewError("SIGNING_UNAVAILABLE", 503))
			return
		}
		seen[choice.ID] = true
		owned[i] = choice
	}
	writeJSON(w, 200, struct {
		Items []provenance.SigningPolicyChoice `json:"items"`
	}{owned})
}
