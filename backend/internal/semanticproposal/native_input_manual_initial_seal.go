package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func (s SQLTransferSourceReader) marshalNativeInputManualInitialAdmissionEvidence(ctx context.Context, who auth.Identity, plan nativeInputManualInitialDraftPlan, built v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeInputManualInitialDraftSeal(ctx, plan); err != nil {
		return nil, err
	}
	owned, err := s.compileNativeInputManualInitialDraft(ctx, who, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return nil, err
	}
	if owned.seal != plan.seal || !reflect.DeepEqual(owned.shell, plan.shell) {
		return nil, v2input.ErrConflict
	}
	stage := owned.prepared.stage
	return s.marshalNativeInputManualAdmissionPacket(ctx, who, owned.shell, owned.prepared.compiled.frozen, stage.dependencies.dependencies, stage.qualification, stage.dependencies.params.params, built)
}

// Registration and complete manual admission share one transaction. Callers
// must roll back errors and commit before returning the queued ticket.
func (s SQLTransferSourceReader) registerAndSealNativeInputManualInitialInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeInputManualInitialDraftPlan, color TransferColorProfile, built v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	packet, err := s.marshalNativeInputManualInitialAdmissionEvidence(ctx, who, plan, built)
	if err != nil {
		return zero, err
	}
	if err := s.registerNativeInputManualInitialInputsInTransaction(ctx, tx, who, plan, color, built); err != nil {
		return zero, err
	}
	return sealNativeInputManualPacketInTransaction(ctx, tx, plan.shell, packet)
}

// Physical reads precede SQL retry transactions. This is the initial blocked
// owner path; an already sealed owner is resumed through saved-plan recovery.
func (s SQLTransferSourceReader) registerAndSealNativeInputManualInitial(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan nativeInputManualInitialDraftPlan, color TransferColorProfile, built v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := s.marshalNativeInputManualInitialAdmissionEvidence(ctx, who, plan, built); err != nil {
		return zero, err
	}
	stage := plan.prepared.stage
	q := stage.qualification.qualification
	current, err := s.prepareNativeInputManualInitialQualification(ctx, who, stage.dependencies, color, q.report.report.ref, q.costs)
	if err != nil {
		return zero, err
	}
	if !sameNativeInputManualInitialStage(ctx, current, stage) {
		return zero, v2input.ErrConflict
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		return s.registerAndSealNativeInputManualInitialInTransaction(ctx, tx, who, plan, color, built)
	})
}
