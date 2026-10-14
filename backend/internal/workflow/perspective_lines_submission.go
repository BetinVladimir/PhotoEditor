package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
	"time"
)

// ErrPerspectiveLinesDraftExpired proves that the run expired before admission.
var ErrPerspectiveLinesDraftExpired = errors.New("perspective line detection draft expired before admission")

// Public intent contains source identity only. Runtime pins, resources, source
// bytes and task IDs are always derived by the server.
type PerspectiveLinesSubmissionIntent struct {
	SchemaVersion int                           `json:"schema_version"`
	Snapshot      uuid.UUID                     `json:"snapshot_id"`
	Base          v2input.SessionVersionBinding `json:"base"`
}

func DecodePerspectiveLinesSubmissionIntent(raw []byte) (PerspectiveLinesSubmissionIntent, error) {
	var zero PerspectiveLinesSubmissionIntent
	if len(raw) < 2 || len(raw) > 8192 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var intent PerspectiveLinesSubmissionIntent
	if json.Unmarshal(normal, &intent) != nil || intent.SchemaVersion != 1 || intent.Snapshot == uuid.Nil || intent.Base.AssetVersionID == uuid.Nil || intent.Base.Source.ArtifactID == uuid.Nil || domainDigestInvalid(intent.Base.Source.SHA256) || domainDigestInvalid(intent.Base.CanvasHash) {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(intent)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	return intent, nil
}

// One key identifies one run independently of the request body. Reusing it
// with another snapshot/base reaches the immutable SQL intent conflict gate.
func PerspectiveLinesSubmissionRun(tenant, project uuid.UUID, who auth.Identity, key string) (uuid.UUID, error) {
	if tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(key) {
		return uuid.Nil, v2input.ErrInvalid
	}
	seed, err := canonical.Marshal(map[string]any{"route": "V29.PerspectiveLines", "project_id": project, "actor_id": who.UserID, "key": key})
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.NewSHA1(tenant, seed), nil
}

// PrepareSubmission resolves accepted source bytes using current snapshot and
// principal authority. It leaves a blocked draft; this is not a queued receipt.
func (s PerspectiveLinesPreparation) PrepareSubmission(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, key string, intent PerspectiveLinesSubmissionIntent) (PreparedPerspectiveLinesDraft, error) {
	var zero PreparedPerspectiveLinesDraft
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	run, err := PerspectiveLinesSubmissionRun(tenant, project, who, key)
	if err != nil {
		return zero, err
	}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		return zero, err
	}
	intent, err = DecodePerspectiveLinesSubmissionIntent(raw)
	if err != nil {
		return zero, err
	}
	if s.DB == nil || s.Inputs == nil {
		return zero, v2input.ErrInvalid
	}
	params := v2optical.PerspectiveParams{SchemaVersion: 1, Base: intent.Base, Mode: "manual", Strength: 0, Corners: []v2optical.PerspectivePoint{{0, 0}, {1, 0}, {1, 1}, {0, 1}}, MaxCropFraction: 1}
	source, err := (v2optical.SQLPerspectiveSourceReader{DB: s.DB, Objects: s.Inputs, Bucket: s.Bucket}).Load(ctx, who, tenant, project, intent.Snapshot, params)
	if err != nil {
		return zero, err
	}
	plan, err := CompilePerspectiveLinesPlan(PerspectiveLinesScope{Tenant: tenant, Project: project, Run: run, Base: intent.Base}, source, s.Policy.workingICC)
	if err != nil {
		return zero, err
	}
	return s.Prepare(ctx, plan, intent.Snapshot, who)
}

// PerspectiveLinesSubmissionReceipt is returned only after SQL confirms a
// sealed admitted graph. Preparing or sealing transport alone is not admission.
type PerspectiveLinesSubmissionReceipt struct {
	Run      uuid.UUID `json:"run_id"`
	Workflow uuid.UUID `json:"workflow_id"`
	Task     uuid.UUID `json:"task_id"`
	State    string    `json:"state"`
}

func (s PerspectiveLinesPreparation) SubmitProjectIntent(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, key string, intent PerspectiveLinesSubmissionIntent) (PerspectiveLinesSubmissionReceipt, error) {
	var zero PerspectiveLinesSubmissionReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	budget := s.SubmissionTimeout
	if budget == 0 {
		budget = 5 * time.Minute
	}
	if budget <= 0 || budget > 5*time.Minute {
		return zero, v2input.ErrInvalid
	}
	ctx, stop := context.WithTimeout(ctx, budget)
	defer stop()
	run, err := PerspectiveLinesSubmissionRun(tenant, project, who, key)
	if err != nil {
		return zero, err
	}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		return zero, err
	}
	intent, err = DecodePerspectiveLinesSubmissionIntent(raw)
	if err != nil {
		return zero, err
	}
	if err := s.bindSubmission(ctx, who, tenant, project, run, key, intent); err != nil {
		return zero, err
	}
	if saved, err := s.storedSubmissionResponse(ctx, who, tenant, project, run, key, intent); err != nil {
		return zero, err
	} else if saved != nil {
		return *saved, nil
	}
	state, err := s.submissionState(ctx, who, tenant, project, run, intent)
	if err != nil {
		return zero, err
	}
	switch state.Stage {
	case "conflict":
		return zero, v2input.ErrConflict
	case "draft_expired":
		return zero, ErrPerspectiveLinesDraftExpired
	case "expired":
		return zero, v2input.ErrExpired
	case "missing", "draft":
		prepared, err := s.PrepareSubmission(ctx, who, tenant, project, key, intent)
		if err != nil {
			return zero, err
		}
		if err = prepared.Seal(ctx, s.DB); err != nil {
			return zero, err
		}
		if err = ActivatePerspectiveLines(ctx, s.DB, prepared.plan, prepared.verified, prepared.bucket); err != nil {
			return zero, err
		}
	case "sealed":
		if err = s.ResumeSealed(ctx, tenant, run); err != nil {
			return zero, err
		}
	}
	saved, err := s.storedSubmissionResponse(ctx, who, tenant, project, run, key, intent)
	if err != nil {
		return zero, err
	}
	if saved == nil {
		return zero, v2input.ErrExpired
	}
	return *saved, nil
}
func perspectiveLinesSubmissionReceipt(run uuid.UUID, state perspectiveLinesSubmissionState) (PerspectiveLinesSubmissionReceipt, error) {
	var zero PerspectiveLinesSubmissionReceipt
	if run == uuid.Nil || state.Task == nil || *state.Task == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	switch state.Stage {
	case "queued", "running", "waiting_review", "completed":
	default:
		return zero, v2input.ErrExpired
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	return PerspectiveLinesSubmissionReceipt{Run: run, Workflow: workflow, Task: *state.Task, State: state.Stage}, nil
}
