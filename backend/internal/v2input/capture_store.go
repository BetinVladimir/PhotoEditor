package v2input

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CaptureAssessmentLease struct {
	PreparationWorkflowID                                                   *uuid.UUID
	JobID, Token, TenantID, ProjectID, ReceiptID, SessionID, AssetVersionID uuid.UUID
	Until                                                                   time.Time
	CreatedAt                                                               time.Time
	Attempts                                                                int
	Policy                                                                  json.RawMessage
}

type SQLCaptureAssessmentStore struct{ DB *pgxpool.Pool }

func validCaptureLease(l CaptureAssessmentLease) bool {
	return l.JobID != uuid.Nil && l.Token != uuid.Nil && l.TenantID != uuid.Nil &&
		l.ProjectID != uuid.Nil && l.ReceiptID != uuid.Nil && l.SessionID != uuid.Nil &&
		l.AssetVersionID != uuid.Nil && !l.Until.IsZero() && !l.CreatedAt.IsZero() && l.Attempts > 0
}

// Claim freezes the receipt's policy; no mutable session policy is re-read.
func (s SQLCaptureAssessmentStore) Claim(ctx context.Context) (*CaptureAssessmentLease, error) {
	if s.DB == nil {
		return nil, ErrInvalid
	}
	var l CaptureAssessmentLease
	err := s.DB.QueryRow(ctx, `SELECT id,lease_token,tenant_id,project_id,receipt_id,session_id,
 asset_version_id,lease_until,created_at,attempts,capture_policy,preparation_workflow_id FROM photo.claim_capture_assessment()`).Scan(
		&l.JobID, &l.Token, &l.TenantID, &l.ProjectID, &l.ReceiptID, &l.SessionID, &l.AssetVersionID, &l.Until, &l.CreatedAt, &l.Attempts, &l.Policy, &l.PreparationWorkflowID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !validCaptureLease(l) || !json.Valid(l.Policy) {
		return nil, ErrInvalid
	}
	return &l, nil
}
func (s SQLCaptureAssessmentStore) Renew(ctx context.Context, l CaptureAssessmentLease) (bool, error) {
	if s.DB == nil || !validCaptureLease(l) {
		return false, ErrInvalid
	}
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT photo.renew_capture_assessment($1,$2)`, l.JobID, l.Token).Scan(&ok)
	return ok, err
}

// Release schedules a bounded retry; a stale owner cannot release a new claim.
func (s SQLCaptureAssessmentStore) Release(ctx context.Context, l CaptureAssessmentLease, delaySeconds int) (bool, error) {
	if s.DB == nil || !validCaptureLease(l) || delaySeconds < 1 || delaySeconds > 300 {
		return false, ErrInvalid
	}
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT photo.release_capture_assessment($1,$2,$3)`, l.JobID, l.Token, delaySeconds).Scan(&ok)
	return ok, err
}

var captureFailureCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,99}$`)

func (s SQLCaptureAssessmentStore) Fail(ctx context.Context, l CaptureAssessmentLease, code string) (bool, error) {
	if s.DB == nil || !validCaptureLease(l) || !captureFailureCode.MatchString(code) {
		return false, ErrInvalid
	}
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT photo.fail_capture_assessment($1,$2,$3)`, l.JobID, l.Token, code).Scan(&ok)
	return ok, err
}

func (s SQLCaptureAssessmentStore) BindPreparation(ctx context.Context, l CaptureAssessmentLease, workflow uuid.UUID) (bool, error) {
	if s.DB == nil || !validCaptureLease(l) || workflow == uuid.Nil {
		return false, ErrInvalid
	}
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT photo.bind_capture_preparation($1,$2,$3,$4)`, l.TenantID, l.JobID, l.Token, workflow).Scan(&ok)
	return ok, err
}

type CapturePreparationProgress struct {
	WorkflowID uuid.UUID
	RunID      uuid.UUID
	Phase      string
}

func (s SQLCaptureAssessmentStore) PreparationProgress(ctx context.Context, l CaptureAssessmentLease) (*CapturePreparationProgress, error) {
	if s.DB == nil || !validCaptureLease(l) {
		return nil, ErrInvalid
	}
	if l.PreparationWorkflowID == nil {
		return nil, nil
	}
	var p CapturePreparationProgress
	err := s.DB.QueryRow(ctx, `SELECT workflow_id,run_id,phase FROM photo.capture_preparation_progress($1,$2,$3)`,
		l.TenantID, l.JobID, l.Token).Scan(&p.WorkflowID, &p.RunID, &p.Phase)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrExpired
	}
	if err != nil {
		return nil, err
	}
	expected, err := WorkflowID(p.RunID)
	if err != nil || expected != p.WorkflowID || p.WorkflowID != *l.PreparationWorkflowID {
		return nil, ErrInvalid
	}
	switch p.Phase {
	case "draft", "pending", "ready", "failed", "cancelled":
	default:
		return nil, ErrInvalid
	}
	return &p, nil
}

func (s SQLCaptureAssessmentStore) FreezeSnapshot(ctx context.Context, l CaptureAssessmentLease, binding SessionVersionBinding) (uuid.UUID, error) {
	if s.DB == nil {
		return uuid.Nil, ErrInvalid
	}
	snapshot, body, _, err := CompileCaptureSnapshot(l, binding)
	if err != nil {
		return uuid.Nil, err
	}
	var actor *uuid.UUID
	err = s.DB.QueryRow(ctx, `SELECT photo.freeze_capture_snapshot($1,$2,$3,$4,$5)`,
		l.TenantID, l.JobID, l.Token, snapshot.SnapshotID, string(body)).Scan(&actor)
	if err != nil {
		return uuid.Nil, err
	}
	if actor == nil || *actor == uuid.Nil {
		return uuid.Nil, ErrInvalid
	}
	return *actor, nil
}

func (s SQLCaptureAssessmentStore) StagePreparation(ctx context.Context, l CaptureAssessmentLease,
	params Ref, digest string, profile json.RawMessage) error {
	if s.DB == nil || !validCaptureLease(l) || !validRef(params) || !digestPattern.MatchString(digest) || !json.Valid(profile) {
		return ErrInvalid
	}
	_, workflow, _, err := CaptureDraftIDs(l.JobID)
	if err != nil {
		return err
	}
	task, err := TaskID(workflow, "capture.prepare", 0)
	if err != nil {
		return err
	}
	var staged bool
	err = s.DB.QueryRow(ctx, `SELECT photo.stage_capture_preparation($1,$2,$3,$4,$5,$6,$7,$8)`,
		l.TenantID, l.JobID, l.Token, task, params.ArtifactID, params.SHA256, digest, profile).Scan(&staged)
	if err != nil {
		return err
	}
	if !staged {
		return ErrInvalid
	}
	return nil
}
