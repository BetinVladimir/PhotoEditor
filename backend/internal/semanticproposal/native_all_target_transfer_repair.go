package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

type nativeTransferRepairState struct {
	sealed, missing bool
	reserved        int64
	registered      []uuid.UUID
}

// Caller holds the original parent advisory lock. Full original/target proofs
// precede eligibility and publication, and repeat before atomic registration.
// This never creates a draft or changes the saved run quota reservation.
func (p TransferDraftPreparer) repairNativeAllTargetTransfer(ctx context.Context, who auth.Identity, request Request, published PublishedIntent, plan nativeAllTargetTransferWorkflowPlan, capacity TransferPreparation, defectBuilt v2input.Built) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.Scheduler == nil {
		return v2input.ErrInvalid
	}
	preparation := plan.reference.person.source.protection.published.preparation
	completed, err := p.Reader.marshalNativeDefectCompletedEvidence(ctx, who, preparation.plan, capacity.Color, preparation.result, defectBuilt)
	if err != nil {
		return err
	}
	lockState := func(tx pgx.Tx) (nativeTransferRepairState, error) {
		var state nativeTransferRepairState
		if err := p.Reader.lockNativeAllTargetTransferAdmissionEvidence(ctx, tx, who, request, published, plan, capacity, completed); err != nil {
			return state, err
		}
		err := tx.QueryRow(ctx, "SELECT graph_sealed,needs_publication,reserved_bytes,registered_tasks FROM photo.lock_semantic_native_targets_transfer_repair_state_v3($1,$2,$3,$4,$5,$6,$7)", request.TenantID, p.Reader.ProjectID, who.UserID, who.SessionID, request.RunID, request.RequestSHA, plan.core.configSHA).Scan(&state.sealed, &state.missing, &state.reserved, &state.registered)
		if err != nil || state.sealed {
			return state, err
		}
		// The v3 barrier already distinguished missing from corrupt registration
		// under task locks. Bind every existing transport to the frozen plan.
		registered := make(map[uuid.UUID]bool, len(state.registered))
		for _, id := range state.registered {
			registered[id] = true
		}
		for i, task := range plan.core.tasks {
			if !registered[task.ID] {
				continue
			}
			var raw []byte
			if err := tx.QueryRow(ctx, "SELECT photo.semantic_native_input_transfer_registered_transport($1,$2,$3,$4,$5)", request.TenantID, request.RunID, task.ID, who.UserID, who.SessionID).Scan(&raw); err != nil {
				return state, err
			}
			if len(raw) == 0 {
				return state, v2input.ErrConflict
			}
			built, err := decodeNativeRecoveredTransport(raw, task.ID)
			if err != nil {
				return state, err
			}
			if _, err := plan.core.frozen[i].BindPublishedTransport(ctx, built); err != nil {
				return state, err
			}
		}
		return state, nil
	}
	var before nativeTransferRepairState
	err = database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error { var err error; before, err = lockState(tx); return err })
	if err != nil {
		return err
	}
	if before.sealed || !before.missing {
		return nil
	}
	if p.TransportDocuments == nil || before.reserved < 1 {
		return v2input.ErrInvalid
	}
	publisher := v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: request.TenantID, Run: request.RunID}
	prepared, err := PublishWorkflowTasks(ctx, plan.core, publisher)
	if err != nil {
		return err
	}
	if err := p.Reader.recheckNativeAllTargetTransferPhysical(ctx, who, request, plan, capacity); err != nil {
		return err
	}
	completed, err = p.Reader.marshalNativeDefectCompletedEvidence(ctx, who, preparation.plan, capacity.Color, preparation.result, defectBuilt)
	if err != nil {
		return err
	}
	transports := make([]v2input.Built, len(prepared))
	for i, task := range prepared {
		transports[i] = task.Graph.Built
	}
	return database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
		after, err := lockState(tx)
		if err != nil {
			return err
		}
		if after.sealed || after.reserved != before.reserved {
			return v2input.ErrConflict
		}
		return p.Reader.registerNativeAllTargetTransferInTransaction(ctx, tx, who, request, published, plan, capacity, completed, transports)
	})
}
