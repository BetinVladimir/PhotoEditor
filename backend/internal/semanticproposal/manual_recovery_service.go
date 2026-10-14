package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// ManualRecoveryService implements the HTTP propose interface for an existing
// original manual parent only. Do not wire it as the general create service:
// missing preparation is refused, never reconstructed by publication.
type ManualRecoveryService struct {
	Preparer TransferDraftPreparer
	Catalog  v2draft.ArtifactCatalog
}

func (s ManualRecoveryService) Propose(ctx context.Context, who auth.Identity, tenant uuid.UUID, key string, intent v2input.SemanticEditIntent) (idempotency.Response, error) {
	var zero idempotency.Response
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || !idempotency.ValidKey(key) {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		return zero, err
	}
	request, err := CompileRequest(tenant, who.UserID, key, raw)
	if err != nil {
		return zero, err
	}
	ticket, err := s.Preparer.loadOriginalAndResumeNativeInputManualTransfer(ctx, s.Catalog, who, tenant, key, request.Body)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return committedManualParentResponse(request, ticket)
}

// Caller obtains this ticket only from complete commit-aware parent admission.
// Matching identities alone is not admission evidence.
func committedManualParentResponse(request Request, ticket v2input.JobTicket) (idempotency.Response, error) {
	if request.RunID == uuid.Nil || request.WorkflowID == uuid.Nil || ticket.RunID != request.RunID || ticket.WorkflowID != request.WorkflowID || ticket.State != "queued" {
		return idempotency.Response{}, v2input.ErrConflict
	}
	body, err := canonical.Marshal(ticket)
	if err != nil {
		return idempotency.Response{}, err
	}
	run := ticket.RunID
	return idempotency.Response{Status: 202, Body: body, ResourceID: &run}, nil
}
