package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Caller owns the scheduler transaction and rolls back every failure. This
// locks retained source, trusted qualification and accepted-version Params;
// parent inventory/snapshot/policy, task registration and activation are separate.
func (s SQLTransferSourceReader) lockSourcePersonPreparation(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, prepared sourcePersonPreparation, expected v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || !prepared.source.unitRGB || prepared.source.iccSHA != color.sha || prepared.source.binding != binding {
		return v2input.ErrInvalid
	}
	_, workflow, err := personPreparationIDs(prepared.parent)
	if err != nil {
		return err
	}
	task, err := v2input.TaskID(workflow, "mask.person", 0)
	if err != nil {
		return err
	}
	draft, err := prepared.catalog.frozen.DraftTask()
	if err != nil {
		return err
	}
	if draft.ID != task {
		return v2input.ErrConflict
	}
	if err := verifyPersonTaskQualificationBinding(prepared.catalog.frozen, prepared.qualification); err != nil {
		return err
	}
	if err := verifyPersonSourceInputPins(ctx, prepared.catalog, prepared.source); err != nil {
		return err
	}
	if _, err := s.marshalPersonQualificationProof(ctx, who, tenant, prepared.qualification, expected); err != nil {
		return err
	}
	if _, err := marshalPersonParamsPin(ctx, s.Bucket, prepared); err != nil {
		return err
	}
	if err := s.lockSourcePreparationProof(ctx, tx, who, tenant, binding, prepared.source); err != nil {
		return err
	}
	if err := s.lockPersonQualificationProof(ctx, tx, who, tenant, prepared.qualification, expected); err != nil {
		return err
	}
	return s.lockPersonParams(ctx, tx, who, tenant, binding, prepared)
}
