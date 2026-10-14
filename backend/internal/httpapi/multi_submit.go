package httpapi

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
)

type MultiSubmit interface {
	SubmitProjectIntent(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, v2optical.MultiIntent) (v2optical.MultiReceipt, error)
}

type MultiDescribe interface {
	DescribeProject(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (v2optical.MultiCapabilities, error)
}

func multiJobTicket(receipt v2optical.MultiReceipt, who auth.Identity, operation string) (semanticProposeTicket, error) {
	raw, err := canonical.Marshal(receipt)
	if err != nil {
		return semanticProposeTicket{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	ticket, err := relightTicket(raw, who)
	if err != nil {
		return ticket, err
	}
	task, err := v2input.TaskID(ticket.WorkflowID, operation, 0)
	if err != nil || (operation != "optical.panorama" && operation != "optical.focus_stack") || receipt.Operation != operation || receipt.Task != task {
		return semanticProposeTicket{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return ticket, nil
}
func (s *Server) multiRoutes(router chi.Router) {
	for _, route := range []struct {
		path, operation string
		service         MultiSubmit
	}{
		{"panorama-intents", "optical.panorama", s.PanoramaSubmit},
		{"focus-stack-intents", "optical.focus_stack", s.FocusStackSubmit},
	} {
		capabilityPath := "panorama-capabilities"
		if route.operation == "optical.focus_stack" {
			capabilityPath = "focus-stack-capabilities"
		}
		router.Get("/api/v1/tenants/{t}/projects/{id}/"+capabilityPath, func(w http.ResponseWriter, r *http.Request) {
			who, tenant, err := s.tenantRequest(r, true)
			if err != nil {
				writeError(w, r, err)
				return
			}
			project, err := uuid.Parse(chi.URLParam(r, "id"))
			if err != nil || project == uuid.Nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			describe, ok := route.service.(MultiDescribe)
			if !ok {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			value, err := describe.DescribeProject(r.Context(), who, tenant, project)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			if value.SchemaVersion != 1 || value.Tenant != tenant || value.Project != project || value.Operation != route.operation {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			writeJSON(w, 200, value)
		})
		router.Post("/api/v1/tenants/{t}/projects/{id}/"+route.path, func(w http.ResponseWriter, r *http.Request) {
			who, tenant, err := s.tenantRequest(r, true)
			if err != nil {
				writeError(w, r, err)
				return
			}
			project, err := uuid.Parse(chi.URLParam(r, "id"))
			if err != nil || project == uuid.Nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			key := r.Header.Get("Idempotency-Key")
			if !idempotency.ValidKey(key) {
				writeError(w, r, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400))
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 66560)
			var raw json.RawMessage
			if err = decodeJSON(r, &raw); err != nil {
				writeError(w, r, err)
				return
			}
			intent, err := v2optical.DecodeMultiIntent(route.operation, raw)
			if err != nil {
				writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
				return
			}
			if route.service == nil {
				writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
				return
			}
			receipt, err := route.service.SubmitProjectIntent(r.Context(), who, tenant, project, key, intent)
			if err != nil {
				writeError(w, r, semanticProposalError(err))
				return
			}
			ticket, err := multiJobTicket(receipt, who, route.operation)
			if err != nil {
				writeError(w, r, err)
				return
			}
			writeJSON(w, 202, ticket)
		})
	}
}
