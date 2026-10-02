package v2learning

import (
	"context"
	"io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type TrainingFitStager interface {
	Stage(context.Context, TrainingDraftPrepared) (TrainingStageResult, error)
	StageFitTask(context.Context, TrainingDraftPrepared,
		LearningPolicy) (FitStageResult, error)
}

type TrainingFitCommitter interface {
	CommitGraph(context.Context, uuid.UUID, uuid.UUID,
		[]v2input.GraphTask) error
}

// TrainingFitOrchestrator seals the initial one-task learning graph only after
// the immutable snapshot, Fit params and transport closure exist. A replayed
// draft is handled by the API's idempotent ticket path, not compiled anew.
type TrainingFitOrchestrator struct {
	Stager    TrainingFitStager
	Transport v2input.Publisher
	Committer TrainingFitCommitter
}

func (o TrainingFitOrchestrator) Admit(ctx context.Context,
	draft TrainingDraftPrepared, policy LearningPolicy) (
	FitStageResult, error) {
	if o.Stager == nil || o.Transport == nil || o.Committer == nil ||
		draft.TenantID == uuid.Nil || draft.RunID == uuid.Nil ||
		draft.WorkflowID == uuid.Nil {
		return FitStageResult{}, ErrInvalidTrainingDraft
	}
	stage, err := o.Stager.Stage(ctx, draft)
	if err != nil {
		return FitStageResult{}, err
	}
	if stage.Outcome == "replayed" {
		return FitStageResult{}, ErrTrainingDraftConflict
	}
	if (stage.Outcome != "created" && stage.Outcome != "pending") ||
		stage.Snapshot.SnapshotID != draft.SnapshotID ||
		stage.Snapshot.SnapshotDigest != draft.SnapshotDigest {
		return FitStageResult{}, ErrTrainingDraftConflict
	}
	fit, err := o.Stager.StageFitTask(ctx, draft, policy)
	if err != nil {
		return FitStageResult{}, err
	}
	task, err := v2input.TaskID(draft.WorkflowID, "learning.fit", 0)
	if err != nil || fit.TaskID != task || fit.Closure.Open == nil ||
		len(fit.Closure.InputDigest) != 64 {
		return FitStageResult{}, ErrInvalidTrainingDraft
	}
	cursor, err := fit.Closure.Open(ctx)
	if err != nil || cursor == nil {
		return FitStageResult{}, ErrInvalidTrainingDraft
	}
	built, buildErr := v2input.BuildInputClosure(ctx, task, cursor,
		o.Transport)
	if closer, okay := cursor.(io.Closer); okay {
		_ = closer.Close()
	}
	if buildErr != nil {
		return FitStageResult{}, buildErr
	}
	graph := []v2input.GraphTask{{ID: task, Built: built,
		Open: fit.Closure.Open, Direct: fit.Closure.Direct}}
	if err := o.Committer.CommitGraph(ctx, draft.TenantID,
		draft.RunID, graph); err != nil {
		return FitStageResult{}, err
	}
	return fit, nil
}

var _ TrainingFitStager = TrainingInputStager{}
var _ TrainingFitCommitter = v2input.Admission{}
