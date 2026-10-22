package semanticreview

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"strconv"
)

// CheckPreparedShownExposure checks exact documents under the caller's Apply
// transaction locks. The caller must separately prove documentRun publication
// authority, decision-batch inclusion and quota, then install/seal atomically.
// Success alone must never queue a task or mark a target applied.
func CheckPreparedShownExposure(ctx context.Context, tx pgx.Tx, who auth.Identity,
	documentRun uuid.UUID, prepared PreparedShownExposure) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || documentRun == uuid.Nil || len(prepared.documents) == 0 ||
		!bytes.Equal(prepared.payload.ParamsJCS, prepared.compiled.body) {
		return ErrInvalid
	}
	if _, err := LockShownExposureParams(ctx, tx, who, prepared.preview, prepared.compiled); err != nil {
		return err
	}
	revision, err := strconv.ParseInt(prepared.preview.WorkflowRevision, 10, 64)
	if err != nil {
		return ErrInvalid
	}
	targetRevision, err := strconv.ParseInt(prepared.preview.TargetRevision, 10, 64)
	if err != nil {
		return ErrInvalid
	}
	p := prepared.compiled.params
	var ready bool
	err = tx.QueryRow(ctx, "SELECT photo.semantic_shown_exposure_admission_documents_ready($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)",
		p.TenantID, prepared.preview.IntentID, who.UserID, who.SessionID, p.ChildRunID, p.TargetID, documentRun,
		revision, targetRevision, string(prepared.compiled.body), prepared.compiled.ParamsRef.SHA256,
		string(prepared.documents), string(prepared.payload.Node), string(prepared.payload.Transport), string(prepared.payload.Inventory)).Scan(&ready)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !ready {
		return ErrConflict
	}
	return nil
}
