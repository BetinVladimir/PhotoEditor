package v2draft

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// LockQualifiedDonorDraft binds newly loaded evidence to the exact frozen
// descriptor and holds current draft/qualification/session/manual lineage locks.
// Callers own the transaction and must roll back any error. Graph sealing and
// registry/queue gates remain separate; this function never activates execution.
func LockQualifiedDonorDraft(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant, project, shoot, run, workflow uuid.UUID,
	ordinal int, frozen FrozenDonorTask, loaded LoadedDonorTaskPlan, prepared PreparedDonorInputEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil || shoot == uuid.Nil || run == uuid.Nil || workflow == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || prepared.qualification == nil {
		return v2input.ErrInvalid
	}
	loaded = ownLoadedDonorTaskPlan(loaded)
	draft, err := frozen.DraftTask()
	if err != nil {
		return err
	}
	rebuilt, err := FreezeQualifiedDonorTask(ctx, workflow, ordinal, loaded, prepared, v2portrait.PublishedDonorTaskPlan{ParamsRef: v2input.Ref{ArtifactID: draft.ParamsArtifactID, SHA256: draft.ParamsSHA}, Plan: loaded.Plan})
	if err != nil {
		return err
	}
	descriptor, err := frozen.Descriptor()
	if err != nil {
		return err
	}
	expected, err := rebuilt.Descriptor()
	if err != nil || !bytes.Equal(descriptor, expected) {
		return v2input.ErrInvalid
	}
	if _, err = CompileDonorManualAdmissionBinding(ctx, loaded, prepared); err != nil {
		return err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var ready bool
	if err = tx.QueryRow(ctx, "SELECT photo.stage_v28_donor_task($1,$2,$3,$4,$5,$6)", tenant, run, who.UserID, who.SessionID, string(descriptor), string(loaded.Plan.Params)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	if err = LockCalibratedDonorSession(ctx, tx, who, tenant, project, shoot, loaded, prepared, *prepared.qualification); err != nil {
		return err
	}
	return LockDonorManualAdmissionBinding(ctx, tx, tenant, run, draft.ID, loaded, prepared)
}
