package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"io"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

// Implementations must verify scoped source/snapshot pins and atomically admit
// qualified transfer tasks with a durable idempotency receipt before returning.
type SemanticPropose interface {
	Propose(context.Context, auth.Identity, uuid.UUID, string, v2input.SemanticEditIntent) (idempotency.Response, error)
}
type semanticProposeTicket struct {
	RunID      uuid.UUID `json:"run_id"`
	WorkflowID uuid.UUID `json:"workflow_id"`
	State      string    `json:"state"`
}

func semanticProposeResponse(response idempotency.Response) (semanticProposeTicket, error) {
	var result semanticProposeTicket
	decoder := json.NewDecoder(bytes.NewReader(response.Body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || response.Status != 202 || result.RunID == uuid.Nil || result.WorkflowID == uuid.Nil || result.State != "queued" || response.ResourceID == nil || *response.ResourceID != result.RunID {
		return semanticProposeTicket{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return result, nil
}
func (s *Server) semanticProposeRoutes(router chi.Router) {
	router.Post("/api/v1/tenants/{t}/semantic-intents", func(w http.ResponseWriter, r *http.Request) {
		who, tenant, err := s.tenantRequest(r, true)
		if err != nil {
			writeError(w, r, err)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !idempotency.ValidKey(key) {
			writeError(w, r, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 65536)
		var raw json.RawMessage
		if err = decodeJSON(r, &raw); err != nil {
			writeError(w, r, err)
			return
		}
		input, err := v2input.DecodeSemanticEditIntent(raw)
		if err != nil {
			writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
			return
		}
		if s.SemanticPropose == nil {
			writeError(w, r, domain.NewError("DEPENDENCY_UNAVAILABLE", 503))
			return
		}
		response, err := s.SemanticPropose.Propose(r.Context(), who, tenant, key, input)
		if err != nil {
			writeError(w, r, semanticProposalError(err))
			return
		}
		result, err := semanticProposeResponse(response)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 202, result)
	})
}
