package v2input

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/database"
)

// CommitCapturePreparation binds the queue job, registers its published closure
// and activates preparation atomically. The job stays retryable while the
// worker runs; its persisted workflow binding prevents duplicate runs.
func (a Admission) CommitCapturePreparation(ctx context.Context, l CaptureAssessmentLease,
	run, workflow uuid.UUID, task GraphTask, params json.RawMessage) error {
	if a.DB == nil || !validCaptureLease(l) || run == uuid.Nil || workflow == uuid.Nil ||
		!json.Valid(params) || len(params) > 1<<20 {
		return ErrInvalid
	}
	expected, err := WorkflowID(run)
	if err != nil || expected != workflow {
		return ErrInvalid
	}
	expected, err = TaskID(workflow, "capture.prepare", 0)
	if err != nil || expected != task.ID {
		return ErrInvalid
	}
	return database.InTransaction(ctx, a.DB, func(tx pgx.Tx) error {
		var bound bool
		if err := tx.QueryRow(ctx, `SELECT photo.bind_capture_preparation($1,$2,$3,$4)`,
			l.TenantID, l.JobID, l.Token, workflow).Scan(&bound); err != nil {
			return err
		}
		if !bound {
			return ErrExpired
		}
		if err := RegisterInTransaction(ctx, tx, l.TenantID, run, task.ID, task.Built, task.Open, task.Direct); err != nil {
			return err
		}
		var sealed bool
		if err := tx.QueryRow(ctx, `SELECT photo.seal_capture_preparation($1,$2,$3,$4,$5)`,
			l.TenantID, l.JobID, l.Token, workflow, string(params)).Scan(&sealed); err != nil {
			return err
		}
		if !sealed {
			return ErrExpired
		}
		return nil
	})
}

// PublishCapturePreparation builds transport objects outside the commit TX.
// The caller supplies a durable blocked draft with already published Params.
func PublishCapturePreparation(ctx context.Context, commit interface {
	CommitCapturePreparation(context.Context, CaptureAssessmentLease, uuid.UUID, uuid.UUID, GraphTask, json.RawMessage) error
}, transport Publisher, l CaptureAssessmentLease, run, workflow uuid.UUID,
	params CapturePrepareParams, paramsRef Ref, source CaptureImageInputs, geometry []Ref) error {
	if commit == nil || transport == nil || !validCaptureLease(l) || params.Binding.AssetVersionID != l.AssetVersionID {
		return ErrInvalid
	}
	expected, err := WorkflowID(run)
	if err != nil || expected != workflow {
		return ErrInvalid
	}
	closure, err := CompileCapturePrepareClosure(workflow, params, paramsRef, source, geometry)
	if err != nil {
		return err
	}
	task, err := TaskID(workflow, "capture.prepare", 0)
	if err != nil {
		return err
	}
	cursor, err := closure.Open(ctx)
	if err != nil {
		return err
	}
	built, err := BuildInputClosure(ctx, task, cursor, transport)
	if err != nil {
		return err
	}
	return commit.CommitCapturePreparation(ctx, l, run, workflow,
		GraphTask{ID: task, Built: built, Open: closure.Open, Direct: closure.Direct}, closure.ParamsJCS)
}
