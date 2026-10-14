package v2draft

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

// LockDonorManualAdmissionBinding repeats original document binding before the
// current SQL producer/result/attempt locks. The caller must retain this same
// transaction through final graph admission and also bind actor/session and
// qualification. No object IO or pixel replay is performed here.
func LockDonorManualAdmissionBinding(ctx context.Context, tx pgx.Tx, tenant, run, task uuid.UUID,
	loaded LoadedDonorTaskPlan, prepared PreparedDonorInputEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || run == uuid.Nil || task == uuid.Nil {
		return v2input.ErrInvalid
	}
	origin, err := CompileDonorManualAdmissionBinding(ctx, loaded, prepared)
	if err != nil {
		return err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var vectorID, vectorSHA any
	if origin.vectors != nil {
		vectorID = origin.vectors.ArtifactID
		vectorSHA = origin.vectors.SHA256
	}
	var rootID, paramsID uuid.UUID
	var rootSHA, paramsSHA string
	err = tx.QueryRow(ctx, "SELECT root_id,root_sha,params_id,params_sha FROM photo.v28_donor_draft_manual_origin($1,$2,$3,$4,$5,$6,$7,$8,$9)", tenant, run, task, origin.targetPixel.ArtifactID, origin.targetPixel.SHA256, origin.guardPixel.ArtifactID, origin.guardPixel.SHA256, vectorID, vectorSHA).Scan(&rootID, &rootSHA, &paramsID, &paramsSHA)
	if errors.Is(err, pgx.ErrNoRows) {
		return v2input.ErrExpired
	}
	if err != nil {
		return err
	}
	if rootID != origin.root.ArtifactID || rootSHA != origin.root.SHA256 || paramsID != origin.original.ArtifactID || paramsSHA != origin.original.SHA256 {
		return v2input.ErrExpired
	}
	return ctx.Err()
}
