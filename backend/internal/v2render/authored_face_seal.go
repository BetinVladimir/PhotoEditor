package v2render

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// SealDetector uses actual generic registration and the PUBLIC installed graph
// seal chain. A refusal from an intervening guard is returned unchanged; no core
// function, version relabel or private fallback is used. It does not dispatch.
func (p AuthoredFacePreparation) SealDetector(ctx context.Context, source AuthoredInstructionSource, prepared v2input.PreparedSemanticTask) error {
	return p.sealNative(ctx, source, "face.detect", prepared)
}
func (p AuthoredFacePreparation) SealLandmarks(ctx context.Context, source AuthoredInstructionSource, prepared v2input.PreparedSemanticTask) error {
	return p.sealNative(ctx, source, "face.landmarks", prepared)
}
func (p AuthoredFacePreparation) SealParser(ctx context.Context, source AuthoredInstructionSource, prepared v2input.PreparedSemanticTask) error {
	return p.sealNative(ctx, source, "face.parse", prepared)
}
func (p AuthoredFacePreparation) sealNative(ctx context.Context, source AuthoredInstructionSource, operation string, prepared v2input.PreparedSemanticTask) error {
	run, workflow, err := AuthoredFacePrerequisiteIDs(source, operation)
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
		if err := tx.QueryRow(ctx, "SELECT photo.authored_render_face_sealed_current($1,$2,$3,$4,$5)", source.Tenant, source.Parent, source.Instruction.Ordinal, operation, task).Scan(&current); err != nil {
			return err
		}
		if current {
			return ctx.Err()
		} // exact sealed/current replay, not dispatch proof.
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT photo.lock_authored_render_face_transport_scope($1,$2,$3,$4,$5)", source.Tenant, source.Parent, source.Instruction.Ordinal, operation, task).Scan(&raw); err != nil {
			return err
		}
		if len(raw) == 0 {
			return v2input.ErrExpired
		}
		if err := v2input.SealGraphInTransaction(ctx, tx, source.Tenant, run, []v2input.GraphTask{prepared.Graph}); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "SELECT photo.authored_render_face_sealed_current($1,$2,$3,$4,$5)", source.Tenant, source.Parent, source.Instruction.Ordinal, operation, task).Scan(&current); err != nil {
			return err
		}
		if !current {
			return v2input.ErrExpired
		}
		return ctx.Err()
	})
}
