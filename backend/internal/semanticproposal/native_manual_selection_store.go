package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Persists only explicit restart locators. No producer or pixel authority is
// conferred; continuation must reverify every original physical manual closure.
func (s SQLTransferSourceReader) saveNativeManualSelection(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, detector detectorDraftPlan, pixels []v2input.Ref, diameter [2]float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if scheduler == nil {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
		return s.saveNativeManualSelectionInTransaction(ctx, tx, who, detector, pixels, diameter)
	})
}

func (s SQLTransferSourceReader) saveNativeManualSelectionInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, detector detectorDraftPlan, pixels []v2input.Ref, diameter [2]float64) error {
	body, _, err := compileNativeManualSelection(ctx, who, detector, pixels, diameter)
	if err != nil {
		return err
	}
	if tx == nil || s.ProjectID != detector.shell.Project {
		return v2input.ErrInvalid
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", detector.request.TenantID.String()); err != nil {
		return err
	}
	var saved bool
	if err := tx.QueryRow(ctx, "SELECT photo.save_v27_native_manual_selection($1,$2,$3,$4,$5,$6)", detector.request.TenantID, detector.shell.Run, who.UserID, who.SessionID, detector.shell.ConfigSHA, string(body)).Scan(&saved); err != nil {
		return err
	}
	if !saved {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

func (s SQLTransferSourceReader) readNativeManualSelection(ctx context.Context, who auth.Identity, detector detectorDraftPlan) (nativeManualSelectionCheckpoint, error) {
	var zero nativeManualSelectionCheckpoint
	if err := verifyDetectorPlanSeal(ctx, detector); err != nil {
		return zero, err
	}
	if s.DB == nil || s.ProjectID != detector.shell.Project {
		return zero, v2input.ErrInvalid
	}
	read := func() (string, string, error) {
		var body, sha string
		err := tenant.WithTenant(ctx, s.DB, who, detector.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
			return tx.QueryRow(ctx, "SELECT selection_body,selection_sha FROM photo.read_v27_native_manual_selection($1,$2,$3,$4,$5,$6)", detector.request.TenantID, s.ProjectID, who.UserID, who.SessionID, detector.request.RunID, detector.shell.ConfigSHA).Scan(&body, &sha)
		})
		return body, sha, err
	}
	body, sha, err := read()
	if err != nil {
		return zero, err
	}
	value, err := decodeNativeManualSelection(ctx, []byte(body), sha, who, detector)
	if err != nil {
		return zero, err
	}
	after, afterSHA, err := read()
	if err != nil {
		return zero, err
	}
	if body != after || sha != afterSHA {
		return zero, v2input.ErrConflict
	}
	return value, ctx.Err()
}
