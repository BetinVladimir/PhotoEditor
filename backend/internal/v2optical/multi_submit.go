package v2optical

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// MultiSubmission is the complete producer-side protocol. Implementations
// must enforce durable idempotency and must never treat receipts as job status.
type MultiSubmission interface {
	PrepareParams(context.Context, auth.Identity, MultiPrepareRequest) (PreparedMulti, error)
	PublishTransport(context.Context, auth.Identity, MultiPrepareRequest, PreparedMulti) (v2input.GraphTask, error)
	Admit(context.Context, auth.Identity, MultiPrepareRequest, PreparedMulti) error
	ReadQueuedReceipt(context.Context, auth.Identity, MultiPrepareRequest) (MultiReceipt, error)
}

// SubmitMulti resumes stable publication reservations and blocked tasks.
// Any ambiguous failure attempts read-only receipt recovery; a failed recovery
// never authorizes activation or a different idempotency key. On cancellation
// the caller retries the same request once a live context is available.
func SubmitMulti(ctx context.Context, workflow MultiSubmission, who auth.Identity, request MultiPrepareRequest) (MultiReceipt, error) {
	var zero MultiReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if workflow == nil {
		return zero, v2input.ErrInvalid
	}
	request = request.owned()
	// Each external stage gets an owned request, preserving the original retry
	// identity even if an injected adapter modifies its input buffers.
	owned := func() MultiPrepareRequest { return request.owned() }
	recoverReceipt := func(cause error) (MultiReceipt, error) {
		if err := ctx.Err(); err != nil {
			return zero, errors.Join(cause, err)
		}
		receipt, err := workflow.ReadQueuedReceipt(ctx, who, owned())
		if err == nil {
			return receipt, nil
		}
		return zero, errors.Join(cause, err)
	}
	prepared, err := workflow.PrepareParams(ctx, who, owned())
	if err != nil {
		return recoverReceipt(err)
	}
	if _, err = workflow.PublishTransport(ctx, who, owned(), prepared); err != nil {
		return recoverReceipt(err)
	}
	if err = workflow.Admit(ctx, who, owned(), prepared); err != nil {
		return recoverReceipt(err)
	}
	return workflow.ReadQueuedReceipt(ctx, who, owned())
}

var _ MultiSubmission = MultiCoordinator{}
