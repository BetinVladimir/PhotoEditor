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

type NegativeSubmit interface {
	SubmitProjectIntent(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, v2optical.NegativeIntent) (v2optical.NegativeReceipt, error)
}

func negativeJobTicket(receipt v2optical.NegativeReceipt, who auth.Identity, intent v2optical.NegativeIntent) (semanticProposeTicket, error) {
	raw, err := canonical.Marshal(receipt)
	if err != nil {
		return semanticProposeTicket{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	ticket, err := relightTicket(raw, who)
	if err != nil {
		return ticket, err
	}
	task, err := v2input.TaskID(ticket.WorkflowID, "optical.negative", 0)
	if err != nil || receipt.Operation != "optical.negative" || receipt.Task != task || receipt.Qualification.Calibration != intent.Params.Calibration {
		return semanticProposeTicket{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return ticket, nil
}
type NegativeCapabilityReader interface {
 ProjectCapabilities(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(v2optical.NegativeCapabilities,error)
}

func (s *Server) negativeRoutes(router chi.Router) {
 router.Get("/api/v1/tenants/{t}/projects/{id}/negative-capabilities",func(w http.ResponseWriter,r *http.Request){
  who,tenant,err:=s.tenantRequest(r,true)
  if err!=nil { writeError(w,r,err);return }
  project,err:=uuid.Parse(chi.URLParam(r,"id"))
  if err!=nil || project==uuid.Nil { writeError(w,r,domain.NewError("VALIDATION_FAILED",422));return }
  reader,ok:=s.NegativeSubmit.(NegativeCapabilityReader)
  if !ok { writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return }
  result,err:=reader.ProjectCapabilities(r.Context(),who,tenant,project)
  if err!=nil { writeError(w,r,semanticProposalError(err));return }
  if result.SchemaVersion!=1 || result.Tenant!=tenant || result.Project!=project || result.Calibration.ArtifactID==uuid.Nil || result.MaxPixels<1 || result.MaxPixels>200000000 { writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return }
  if _,err:=domain.ParseDigest(result.Calibration.SHA256);err!=nil { writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return }
  w.Header().Set("Cache-Control","no-store")
  writeJSON(w,200,result)
 })
	router.Post("/api/v1/tenants/{t}/projects/{id}/negative-intents", func(w http.ResponseWriter, r *http.Request) {
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
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		var raw json.RawMessage
		if err = decodeJSON(r, &raw); err != nil {
			writeError(w, r, err)
			return
		}
		intent, err := v2optical.DecodeNegativeIntent(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.NegativeSubmit == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		receipt, err := s.NegativeSubmit.SubmitProjectIntent(r.Context(), who, tenant, project, key, intent)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		ticket, err := negativeJobTicket(receipt, who, intent)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 202, ticket)
	})
}
