package v2input

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/database"
)

// CommitCapture registers the already published closure and seals its graph
// in the same transaction as the queue transition. Failure leaves tasks blocked.
func (a Admission) CommitCapture(ctx context.Context, lease CaptureAssessmentLease,
	run, workflow uuid.UUID, task GraphTask, params, contextBody json.RawMessage) error {
	if a.DB == nil || !validCaptureLease(lease) || run == uuid.Nil || workflow == uuid.Nil ||
		!json.Valid(params) || !json.Valid(contextBody) || len(params) > 8<<20 || len(contextBody) > 8<<20 {
		return ErrInvalid
	}
	expected, err := TaskID(workflow, "capture.assess", 0)
	if err != nil || expected != task.ID {
		return ErrInvalid
	}
	return database.InTransaction(ctx, a.DB, func(tx pgx.Tx) error {
		var locked bool
		if err := tx.QueryRow(ctx, `SELECT photo.lock_capture_assessment_commit($1,$2,$3,$4,$5)`,
			lease.TenantID, lease.JobID, lease.Token, run, workflow).Scan(&locked); err != nil {
			return err
		}
		if !locked {
			return ErrExpired
		}
		if err := RegisterInTransaction(ctx, tx, lease.TenantID, run, task.ID, task.Built, task.Open, task.Direct); err != nil {
			return err
		}
		var closed bool
		err := tx.QueryRow(ctx, `SELECT photo.close_capture_assessment($1,$2,$3,$4,$5,$6)`,
			lease.TenantID, lease.JobID, lease.Token, workflow, string(params), string(contextBody)).Scan(&closed)
		if err != nil {
			return err
		}
		if !closed {
			return fmt.Errorf("%w: capture admission rejected", ErrExpired)
		}
		return nil
	})
}
