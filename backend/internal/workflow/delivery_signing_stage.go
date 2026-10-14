package workflow

import (
	"context"
	"time"

	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// DeliverySigningStage joins bounded scheduler dispatch with whole-matrix
// physically verified closure retention. A queued child never advances the
// assembly processor; only the coordinator's adopted result may do so.
type DeliverySigningStage struct {
	Store         delivery.ExpansionLeaseStore
	Observations  delivery.MatrixObservationStore
	Dispatcher    delivery.DelegatedSigningSlotDispatcher
	Closure       delivery.DelegatedSigningClosureCoordinator
	Failure       DeliveryFailureWriter
	SignedFailure DeliverySignedFailureWriter
	PageSize      int
	ExpiredPolicy interface {
		FailExpiredPolicy(context.Context, delivery.ExpansionLease, delivery.PreparePlan) (bool, error)
	}
}

func (s DeliverySigningStage) ProcessSigning(ctx context.Context,
	lease delivery.ExpansionLease, plan delivery.PreparePlan) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.Store.DB == nil || s.Observations == nil ||
		s.ExpiredPolicy == nil ||
		s.PageSize < 1 || s.PageSize > 100 ||
		!lease.Valid() || lease.ParentTask != plan.TaskID() {
		return "", v2input.ErrInvalid
	}
	expired, err := s.ExpiredPolicy.FailExpiredPolicy(ctx, lease, plan)
	if err != nil {
		return "", err
	}
	if expired {
		return "finalized_failed", nil
	}
	_, pageFailure, sweepComplete, err := delivery.ProcessCurrentSigningPageWithFailure(ctx, s.Store,
		s.Observations, plan, lease, s.PageSize,
		s.Dispatcher.Dispatch)
	if err != nil {
		return "", err
	}
	if pageFailure != nil {
		if s.SignedFailure == nil {
			return "", v2input.ErrInvalid
		}
		if err := s.Observations.Renew(ctx, lease); err != nil {
			return "", err
		}
		if err := s.SignedFailure.FailSignedPackage(ctx, lease, plan,
			*pageFailure); err != nil {
			return "", err
		}
		return "finalized_failed", nil
	}
	if !sweepComplete {
		return "scanning", nil
	}
	type closureResult struct {
		state    string
		evidence delivery.VerifiedDelegatedSigningSet
	}
	closure, err := withDeliveryPhysicalLease(ctx, s.Observations, lease, 10*time.Second,
		func(work context.Context) (closureResult, error) {
			state, evidence, err := s.Closure.VerifyAndRetainEvidence(work, plan, lease)
			return closureResult{state: state, evidence: evidence}, err
		})
	if err != nil {
		return "", err
	}
	state, evidence := closure.state, closure.evidence
	if state == "failed" {
		if evidence.FailedSlot != nil {
			if s.SignedFailure == nil {
				return "", v2input.ErrInvalid
			}
			if err := s.Observations.Renew(ctx, lease); err != nil {
				return "", err
			}
			if err := s.SignedFailure.FailSignedPackage(ctx, lease, plan,
				*evidence.FailedSlot); err != nil {
				return "", err
			}
			return "finalized_failed", nil
		}
		if s.Failure == nil {
			return "", v2input.ErrInvalid
		}
		finalized, err := FinalizeCurrentDeliveryFailure(ctx, s.Observations,
			s.Failure, lease, plan)
		if err != nil {
			return "", err
		}
		if finalized {
			return "finalized_failed", nil
		}
		return "pending", nil
	}
	if state != "pending" && state != "adopted" {
		return "", v2input.ErrInvalid
	}
	return state, nil
}

var _ DeliverySigningProcessor = DeliverySigningStage{}
