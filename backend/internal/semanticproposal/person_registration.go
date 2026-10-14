package semanticproposal

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Registers only exact immutable transport after re-locking original authority.
// Caller rolls back errors; no publication or queue activation in this TX.
func (s SQLTransferSourceReader) registerPersonInputsInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan personDraftPlan, color TransferColorProfile, built v2input.Built) error {
	if err := verifyPersonPlanSeal(ctx, plan); err != nil {
		return err
	}
	prepared, err := plan.prepared.catalog.frozen.BindPublishedTransport(ctx, built)
	if err != nil {
		return err
	}
	if _, err := s.beginPersonDraftInTransaction(ctx, tx, who, plan, color); err != nil {
		return err
	}
	draft, err := plan.prepared.catalog.frozen.DraftTask()
	if err != nil {
		return err
	}
	request := plan.request
	request.RunID = plan.shell.Run
	state, err := personPreparationRegistrationState(ctx, tx, request, draft, prepared.Graph)
	if err != nil {
		return err
	}
	if state == "replayed" {
		return nil
	}
	if state != "missing" {
		return fmt.Errorf("person registration state %s: %w", state, v2input.ErrConflict)
	}
	if err := v2input.RegisterInTransaction(ctx, tx, request.TenantID, request.RunID, draft.ID, built, prepared.Graph.Open, prepared.Graph.Direct); err != nil {
		return err
	}
	state, err = personPreparationRegistrationState(ctx, tx, request, draft, prepared.Graph)
	if err != nil {
		return err
	}
	if state != "replayed" {
		return v2input.ErrConflict
	}
	return nil
}

func personPreparationRegistrationState(ctx context.Context, tx pgx.Tx, request Request, draft v2input.DraftTask, task v2input.GraphTask) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if tx == nil || request.TenantID == uuid.Nil || request.RunID == uuid.Nil ||
		draft.ID == uuid.Nil || draft.ID != task.ID || draft.Operation != "mask.person" ||
		draft.OperationVersion != "1" || draft.ResourceClass != "gpu" {
		return "", v2input.ErrInvalid
	}
	proof, err := freezeDefectRegistrationProof(ctx, task)
	if err != nil {
		return "", err
	}
	var state string
	err = tx.QueryRow(ctx, "SELECT photo.semantic_person_preparation_registration_state($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", request.TenantID, request.RunID, task.ID, draft.ParamsArtifactID, draft.ParamsSHA, draft.InputDigest, draft.ExecutionProfile, proof.Rows, proof.Manifest, proof.Chunks, proof.Direct, proof.Count, proof.EntriesSHA).Scan(&state)
	return state, err
}
