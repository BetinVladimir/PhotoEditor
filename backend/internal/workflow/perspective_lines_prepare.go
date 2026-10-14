package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
)

// PerspectiveLinesPreparation owns the dependencies for the draft-only phase.
// Retrying after transport seal requires a separate durable recovery path.
type PerspectiveLinesPreparation struct {
	DB                *pgxpool.Pool
	Documents         artifactjson.Objects
	Inputs            v2input.SessionAnalysisObjectReader
	Sources           PerspectiveLinesSourceCatalog
	Arrays            v2optical.PerspectiveSourceArrayCatalog
	Bucket            string
	Policy            PerspectiveLinesExecutionPolicy
	SubmissionTimeout time.Duration
}

// PreparedPerspectiveLinesDraft retains the original closure and verified IO
// witness for seal. Its private fields prevent construction from request JSON.
type PreparedPerspectiveLinesDraft struct {
	plan     PerspectiveLinesPlan
	task     PreparedTask
	closure  PerspectiveLinesClosure
	verified VerifiedPerspectiveLinesInputs
	workflow uuid.UUID
	bucket   string
}

func (p PreparedPerspectiveLinesDraft) WorkflowID() uuid.UUID { return p.workflow }

// Prepare publishes a blocked draft, never dispatches it. On failure durable
// reservations are retained for retry/terminal cleanup, not released while
// documents may still be publishing in another request.
func (s PerspectiveLinesPreparation) Prepare(ctx context.Context, plan PerspectiveLinesPlan, snapshot uuid.UUID, who auth.Identity) (PreparedPerspectiveLinesDraft, error) {
	var zero PreparedPerspectiveLinesDraft
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Documents == nil || s.Inputs == nil || s.Sources == nil || s.Arrays == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return zero, v2input.ErrInvalid
	}
	if _, err := CompilePerspectiveLinesIntent(plan, snapshot, who); err != nil {
		return zero, err
	}
	if _, err := s.Policy.ForPlan(plan); err != nil {
		return zero, err
	}
	store := PerspectiveLinesIntentStore{DB: s.DB}
	if _, err := store.Save(ctx, plan, snapshot, who); err != nil {
		return zero, fmt.Errorf("save line intent: %w", err)
	}
	reserved, err := store.Reserve(ctx, plan)
	if err != nil {
		return zero, fmt.Errorf("reserve line quota: %w", err)
	}
	if !reserved {
		return zero, v2input.ErrExpired
	}
	task, err := PublishPerspectiveLinesTask(ctx, s.DB, s.Documents, s.Bucket, plan)
	if err != nil {
		return zero, fmt.Errorf("publish line params: %w", err)
	}
	if err := MaterializePerspectiveLinesTask(ctx, s.DB, plan, task); err != nil {
		return zero, fmt.Errorf("materialize line task: %w", err)
	}
	closure, err := PublishPerspectiveLinesClosure(ctx, s.DB, s.Documents, s.Bucket, plan, task.Params)
	if err != nil {
		return zero, fmt.Errorf("publish line closure: %w", err)
	}
	verified, err := VerifyPerspectiveLinesInputs(ctx, s.Sources, s.Arrays, s.Inputs, plan, snapshot, who)
	if err != nil {
		return zero, fmt.Errorf("verify line source: %w", err)
	}
	workflow, err := CreatePerspectiveLinesWorkflow(ctx, s.DB, plan, s.Policy, verified, s.Bucket)
	if err != nil {
		return zero, fmt.Errorf("create line workflow: %w", err)
	}
	return PreparedPerspectiveLinesDraft{plan: plan, task: task, closure: closure, verified: verified, workflow: workflow, bucket: s.Bucket}, nil
}

// Seal stores the transport receipt without activating the graph. Fresh SQL
// authority is still mandatory during the separate activation transaction.
func (p PreparedPerspectiveLinesDraft) Seal(ctx context.Context, db *pgxpool.Pool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	expected, err := v2input.WorkflowID(p.plan.scope.Run)
	if err != nil || p.workflow == uuid.Nil || p.workflow != expected {
		return v2input.ErrInvalid
	}
	return SealPerspectiveLinesTransport(ctx, db, p.plan, p.task.Params, p.closure, p.verified, p.bucket)
}
