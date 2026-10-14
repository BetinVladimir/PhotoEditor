package v2learning

import (
	"context"
	"io"
	"time"

	"photoeditor/backend/internal/v2input"
)

// LearningEvaluateSource must return the immutable training draft and the
// transitive holdout inputs pinned to that draft, never live editor state.
type LearningEvaluateSource interface {
	Load(context.Context, LearningEvaluateLease) (TrainingDraftPrepared, []LearningRenderInputs, error)
}

type LearningEvaluateLeases interface {
	Claim(context.Context) (*LearningEvaluateLease, error)
	Renew(context.Context, LearningEvaluateLease) (bool, error)
	Release(context.Context, LearningEvaluateLease) (bool, error)
}

type LearningEvaluateStager interface {
	StageEvaluateTask(context.Context, LearningEvaluateLease,
		TrainingDraftPrepared, []LearningRenderInputs) (EvaluateStageResult, error)
}

type LearningEvaluateCommitter interface {
	CommitEvaluateClosure(context.Context, LearningEvaluateLease, v2input.GraphTask) error
}

// LearningEvaluateOrchestrator performs object IO between fenced database
// steps. The barrier closes only after the exact transport closure is ready.
type LearningEvaluateOrchestrator struct {
	Leases            LearningEvaluateLeases
	Source            LearningEvaluateSource
	Stager            LearningEvaluateStager
	TransportForLease func(LearningEvaluateLease) v2input.Publisher
	Committer         LearningEvaluateCommitter
}

func (o LearningEvaluateOrchestrator) ProcessOne(ctx context.Context) (bool, error) {
	return o.RunOnce(ctx)
}

func (o LearningEvaluateOrchestrator) RunOnce(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if o.Leases == nil || o.Source == nil || o.Stager == nil ||
		o.TransportForLease == nil || o.Committer == nil {
		return false, ErrInvalidTrainingDraft
	}
	lease, err := o.Leases.Claim(ctx)
	if err != nil || lease == nil {
		return false, err
	}
	completed := false
	defer func() {
		if !completed {
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_, _ = o.Leases.Release(releaseCtx, *lease)
		}
	}()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !validLearningEvaluateLease(*lease) {
		return false, ErrInvalidTrainingDraft
	}
	transport := o.TransportForLease(*lease)
	if transport == nil {
		return false, ErrInvalidTrainingDraft
	}
	draft, resolved, err := o.Source.Load(ctx, *lease)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if draft.TenantID != lease.TenantID || draft.RunID != lease.RunID ||
		draft.WorkflowID != lease.WorkflowID {
		return false, ErrInvalidTrainingDraft
	}
	if okay, err := o.Leases.Renew(ctx, *lease); err != nil || !okay {
		if err != nil {
			return false, err
		}
		return false, ErrTrainingDraftConflict
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	stage, err := o.Stager.StageEvaluateTask(ctx, *lease, draft, resolved)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	expectedTask, err := v2input.TaskID(lease.WorkflowID, "learning.evaluate", 0)
	if err != nil || stage.TaskID != expectedTask || stage.Closure.Open == nil ||
		len(stage.Closure.InputDigest) != 64 {
		return false, ErrInvalidTrainingDraft
	}
	cursor, err := stage.Closure.Open(ctx)
	if err != nil {
		if closer, okay := cursor.(io.Closer); okay {
			_ = closer.Close()
		}
		return false, err
	}
	if cursor == nil {
		return false, ErrInvalidTrainingDraft
	}
	built, buildErr := v2input.BuildInputClosure(ctx, stage.TaskID, cursor, transport)
	if closer, okay := cursor.(io.Closer); okay {
		if closeErr := closer.Close(); buildErr == nil {
			buildErr = closeErr
		}
	}
	if buildErr != nil {
		return false, buildErr
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if okay, err := o.Leases.Renew(ctx, *lease); err != nil || !okay {
		if err != nil {
			return false, err
		}
		return false, ErrTrainingDraftConflict
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := o.Committer.CommitEvaluateClosure(ctx, *lease, v2input.GraphTask{
		ID: stage.TaskID, Built: built, Open: stage.Closure.Open,
		Direct: stage.Closure.Direct}); err != nil {
		return false, err
	}
	completed = true
	return true, nil
}

var _ LearningEvaluateLeases = SQLLearningEvaluateStore{}
var _ LearningEvaluateCommitter = SQLLearningEvaluateStore{}
var _ LearningEvaluateStager = TrainingInputStager{}
