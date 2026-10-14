package semanticproposal

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

// CreateService owns deployment policy. Source/target protection locators are
// selected from canonical original producers, never supplied by the caller.
// Missing producers or qualification refuse admission; blocked drafts and child
// receipts never become successful HTTP responses.
type CreateService struct{ Preparer TransferDraftPreparer }

func (s CreateService) Propose(ctx context.Context, who auth.Identity, tenant uuid.UUID, key string, intent v2input.SemanticEditIntent) (idempotency.Response, error) {
	var zero idempotency.Response
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || !idempotency.ValidKey(key) {
		return zero, v2input.ErrInvalid
	}
	p := s.Preparer
	p.Configuration = ownTransferPreparation(p.Configuration)
	if p.Configuration.PersonProtection == nil || p.Configuration.SourceProtection != (v2input.Ref{}) || len(p.Configuration.TargetProtection) != 0 {
		return zero, v2input.ErrInvalid
	}
	if err := validateTransferPolicyConfiguration(p.Configuration); err != nil {
		return zero, err
	}
	if err := validateCreatePreparer(ctx, p); err != nil {
		return zero, err
	}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		return zero, err
	}
	request, err := CompileRequest(tenant, who.UserID, key, raw)
	if err != nil {
		return zero, err
	}
	published, err := PublishIntent(ctx, p.Documents, who, request)
	if err != nil {
		return zero, err
	}
	configuration, err := p.Reader.selectOrdinaryTransferConfiguration(ctx, who, request, published, p.Configuration)
	if err != nil {
		if errors.Is(err, errCreateReferencePersonUnavailable) {
			if _, preparationErr := p.prepareMissingCreatePerson(ctx, who, request, key, published); preparationErr != nil {
				return zero, preparationErr
			}
		}
		// Successful child admission remains private; caller retries the same parent
		// request after actual canonical person output becomes available.
		return zero, err
	}
	return p.admitOrdinaryCreateRequest(ctx, who, request, key, configuration)
}

func (p TransferDraftPreparer) admitOrdinaryCreateRequest(ctx context.Context, who auth.Identity, request Request, key string, configuration TransferPreparation) (idempotency.Response, error) {
	var zero idempotency.Response
	p.Configuration = configuration
	prepared, err := p.Prepare(ctx, who, request.TenantID, key, request.Body)
	if err != nil {
		return zero, err
	}
	var ticket v2input.JobTicket
	if prepared.Outcome == "replayed" {
		ticket, err = p.replayPreparedTransfer(ctx, who, request, key, prepared)
	} else {
		ticket, err = p.admitPreparedTransfer(ctx, who, request, key, prepared)
	}
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return committedManualParentResponse(request, ticket)
}
