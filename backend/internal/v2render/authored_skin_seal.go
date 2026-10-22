package v2render

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

func (p AuthoredSkinPreparation) seal(ctx context.Context, source AuthoredInstructionSource, operation string, prepared v2input.PreparedSemanticTask) error {
	run, workflow, err := AuthoredSkinPrerequisiteIDs(source, operation)
	if err != nil {
		return err
	}
	task, err := v2input.TaskID(workflow, operation, 0)
	if err != nil {
		return err
	}
	if p.Scheduler == nil || prepared.Graph.ID != task {
		return v2input.ErrInvalid
	}
	return database.TenantTransaction(ctx, p.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var current bool
		if err := tx.QueryRow(ctx, "SELECT photo.authored_render_skin_sealed_current($1,$2,$3,$4,$5)", source.Tenant, source.Parent, source.Instruction.Ordinal, operation, task).Scan(&current); err != nil {
			return err
		}
		if current {
			return ctx.Err()
		} // exact sealed/current replay, not dispatch proof.
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT photo.lock_authored_render_skin_transport_scope($1,$2,$3,$4,$5)", source.Tenant, source.Parent, source.Instruction.Ordinal, operation, task).Scan(&raw); err != nil {
			return err
		}
		if len(raw) == 0 {
			return v2input.ErrExpired
		}
		if err := v2input.SealGraphInTransaction(ctx, tx, source.Tenant, run, []v2input.GraphTask{prepared.Graph}); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "SELECT photo.authored_render_skin_sealed_current($1,$2,$3,$4,$5)", source.Tenant, source.Parent, source.Instruction.Ordinal, operation, task).Scan(&current); err != nil {
			return err
		}
		if !current {
			return v2input.ErrExpired
		}
		return ctx.Err()
	})
}
