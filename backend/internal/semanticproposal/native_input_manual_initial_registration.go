package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Exact blocked-state registration under complete original owner/evidence locks.
// Every error requires rollback; activation is a separate atomic manual gate.
func (s SQLTransferSourceReader) registerNativeInputManualInitialInputsInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeInputManualInitialDraftPlan, color TransferColorProfile, built v2input.Built) error {
	if err := verifyNativeInputManualInitialDraftSeal(ctx, plan); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	owned, err := s.compileNativeInputManualInitialDraft(ctx, who, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return err
	}
	if owned.seal != plan.seal || !reflect.DeepEqual(owned.shell, plan.shell) {
		return v2input.ErrConflict
	}
	prepared, err := owned.prepared.compiled.frozen.BindPublishedTransport(ctx, built)
	if err != nil {
		return err
	}
	binding, err := marshalNativeDefectTaskRegistrationBinding(ctx, owned.prepared.compiled.frozen, owned.shell, built)
	if err != nil {
		return err
	}
	if _, err := s.beginNativeInputManualDraftInTransaction(ctx, tx, who, owned, color); err != nil {
		return err
	}
	state := func() (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var out string
		err := tx.QueryRow(ctx, "SELECT photo.semantic_native_defect_registration_binding_state($1,$2,$3,$4::jsonb)", owned.shell.Tenant, owned.shell.Run, "defects.propose", string(binding)).Scan(&out)
		return out, err
	}
	current, err := state()
	if err != nil {
		return err
	}
	if current == "replayed" {
		return ctx.Err()
	}
	if current != "missing" {
		return v2input.ErrConflict
	}
	if err := v2input.RegisterInTransaction(ctx, tx, owned.shell.Tenant, owned.shell.Run, prepared.Graph.ID, built, prepared.Graph.Open, prepared.Graph.Direct); err != nil {
		return err
	}
	current, err = state()
	if err != nil {
		return err
	}
	if current != "replayed" {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
