package v2optical

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// PerspectiveSubmission is the complete producer-side protocol. Implementations
// must enforce durable idempotency and must never treat receipts as job status.
type PerspectiveSubmission interface {
	PrepareParams(context.Context, auth.Identity, PerspectivePrepareRequest) (PreparedPerspective, error)
	PublishTransport(context.Context, auth.Identity, PerspectivePrepareRequest, PreparedPerspective) (v2input.GraphTask, error)
	Admit(context.Context, auth.Identity, PerspectivePrepareRequest, PreparedPerspective) error
	ReadQueuedReceipt(context.Context, auth.Identity, PerspectivePrepareRequest) (PerspectiveReceipt, error)
}

// SubmitPerspective resumes stable publication reservations and blocked tasks.
// Any ambiguous failure attempts read-only receipt recovery; a failed recovery
// never authorizes activation or a different idempotency key. On cancellation
// the caller retries the same request once a live context is available.
func SubmitPerspective(ctx context.Context, workflow PerspectiveSubmission, who auth.Identity, request PerspectivePrepareRequest) (PerspectiveReceipt, error) {
	var zero PerspectiveReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if workflow == nil {
		return zero, v2input.ErrInvalid
	}
	request.Params = append([]byte(nil), request.Params...)
	request.Source = append([]byte(nil), request.Source...)
	// Each external stage gets an owned request, preserving the original retry
	// identity even if an injected adapter modifies its input buffers.
	owned := func() PerspectivePrepareRequest {
		r := request
		r.Params = append([]byte(nil), request.Params...)
		r.Source = append([]byte(nil), request.Source...)
		return r
	}
	recoverReceipt := func(cause error) (PerspectiveReceipt, error) {
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

var _ PerspectiveSubmission = PerspectiveCoordinator{}
