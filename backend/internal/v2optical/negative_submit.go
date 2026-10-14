package v2optical

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// NegativeSubmission is the complete producer-side protocol. Implementations
// must enforce durable idempotency and must never treat receipts as job status.
type NegativeSubmission interface {
	PrepareParams(context.Context, auth.Identity, NegativePrepareRequest) (PreparedNegative, error)
	PublishTransport(context.Context, auth.Identity, NegativePrepareRequest, PreparedNegative) (v2input.GraphTask, error)
	Admit(context.Context, auth.Identity, NegativePrepareRequest, PreparedNegative) error
	ReadQueuedReceipt(context.Context, auth.Identity, NegativePrepareRequest) (NegativeReceipt, error)
}

// SubmitNegative resumes stable publication reservations and blocked tasks.
// Any ambiguous failure attempts read-only receipt recovery; a failed recovery
// never authorizes activation or a different idempotency key. On cancellation
// the caller retries the same request once a live context is available.
func SubmitNegative(ctx context.Context, workflow NegativeSubmission, who auth.Identity, request NegativePrepareRequest) (NegativeReceipt, error) {
	var zero NegativeReceipt
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
	owned := func() NegativePrepareRequest {
		r := request
		r.Params = append([]byte(nil), request.Params...)
		r.Source = append([]byte(nil), request.Source...)
		return r
	}
	recoverReceipt := func(cause error) (NegativeReceipt, error) {
		if err := ctx.Err(); err != nil {
			return zero, errors.Join(cause, err)
		}
		receipt, err := workflow.ReadQueuedReceipt(ctx, who, owned())
		if err == nil {
			return receipt, nil
		}
		return zero, errors.Join(cause, err)
	}
 // Recover an already acknowledged request before any reservation or PUT.
 // Expired also covers a stale saved receipt: subsequent draft-save authority
 // must reject sealed/stale runs, so this never authorizes a second activation.
 if receipt,err:=workflow.ReadQueuedReceipt(ctx,who,owned());err==nil { return receipt,nil } else if !errors.Is(err,v2input.ErrExpired) { return zero,err }
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

var _ NegativeSubmission = NegativeCoordinator{}
