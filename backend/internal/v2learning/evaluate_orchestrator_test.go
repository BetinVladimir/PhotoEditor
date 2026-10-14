package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type evaluationLeases struct {
	lease            LearningEvaluateLease
	renews, releases int
	denyRenew        bool
	cancelAt string
	cancel context.CancelFunc
}

func (s *evaluationLeases) Claim(context.Context) (*LearningEvaluateLease, error) {
	if s.cancelAt == "claim" { s.cancel() }
	return &s.lease, nil
}
func (s *evaluationLeases) Renew(context.Context, LearningEvaluateLease) (bool, error) {
	s.renews++
	if s.cancelAt == "renew" { s.cancel() }
	return !s.denyRenew, nil
}
func (s *evaluationLeases) Release(context.Context, LearningEvaluateLease) (bool, error) {
	s.releases++
	return true, nil
}

type evaluationSource struct {
	draft TrainingDraftPrepared
	cancel context.CancelFunc
}

func (s evaluationSource) Load(context.Context, LearningEvaluateLease) (TrainingDraftPrepared, []LearningRenderInputs, error) {
	if s.cancel != nil { s.cancel() }
	return s.draft, nil, nil
}

type evaluationCursor struct {
	entries []v2input.Entry
	next    int
}

func (c *evaluationCursor) Next(context.Context) (v2input.Entry, bool, error) {
	if c.next == len(c.entries) {
		return v2input.Entry{}, false, nil
	}
	entry := c.entries[c.next]
	c.next++
	return entry, true, nil
}

type evaluationStager struct {
	task  uuid.UUID
	calls int
	fail  bool
	cancel context.CancelFunc
}

func (s *evaluationStager) StageEvaluateTask(_ context.Context,
	lease LearningEvaluateLease, draft TrainingDraftPrepared,
	_ []LearningRenderInputs) (EvaluateStageResult, error) {
	s.calls++
	if s.cancel != nil { s.cancel() }
	if s.fail {
		return EvaluateStageResult{}, errors.New("stage failed")
	}
	if draft.RunID != lease.RunID {
		return EvaluateStageResult{}, ErrInvalidTrainingDraft
	}
	entry := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(),
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Contract: "Params"}
	return EvaluateStageResult{TaskID: s.task, Closure: FitClosure{
		Open: func(context.Context) (v2input.Cursor, error) {
			return &evaluationCursor{entries: []v2input.Entry{entry}}, nil
		}, InputDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Direct: []v2input.Ref{entry.Ref}}}, nil
}

type evaluationTransport struct{}

func (evaluationTransport) Publish(_ context.Context, _ uuid.UUID, _ int, _ string,
	body []byte) (v2input.Ref, error) {
	sum := sha256.Sum256(body)
	return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, nil
}

type evaluationCommitter struct {
	calls int
	fail  bool
}

func (c *evaluationCommitter) CommitEvaluateClosure(_ context.Context,
	_ LearningEvaluateLease, task v2input.GraphTask) error {
	if c.fail {
		return errors.New("commit failed")
	}
	if task.Built.Count != 1 || task.Open == nil || len(task.Direct) != 1 {
		return ErrInvalidTrainingDraft
	}
	c.calls++
	return nil
}

func TestLearningEvaluateOrchestratorFencesStageAndCommitsClosure(t *testing.T) {
	lease := LearningEvaluateLease{ExpansionID: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Minute), TenantID: uuid.New(), RunID: uuid.New(),
		WorkflowID: uuid.New(), Candidate: v2input.Ref{ArtifactID: uuid.New(),
			SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}}
	task, err := v2input.TaskID(lease.WorkflowID, "learning.evaluate", 0)
	if err != nil {
		t.Fatal(err)
	}
	leasors := &evaluationLeases{lease: lease}
	stager := &evaluationStager{task: task}
	committer := &evaluationCommitter{}
	o := LearningEvaluateOrchestrator{Leases: leasors,
		Source: evaluationSource{draft: TrainingDraftPrepared{
			TenantID: lease.TenantID, RunID: lease.RunID,
			WorkflowID: lease.WorkflowID}}, Stager: stager,
		TransportForLease: func(LearningEvaluateLease) v2input.Publisher {
			return evaluationTransport{}
		}, Committer: committer}
	okay, err := o.RunOnce(context.Background())
	if err != nil || !okay || leasors.renews != 2 || leasors.releases != 0 ||
		stager.calls != 1 || committer.calls != 1 {
		t.Fatalf("evaluation closure not committed: %v %v %+v", okay, err, leasors)
	}
	stager.fail = true
	okay, err = o.RunOnce(context.Background())
	if okay || err == nil || leasors.releases != 1 || committer.calls != 1 {
		t.Fatalf("failed stage did not release lease: %v %v %+v", okay, err, leasors)
	}
	stager.fail = false
	leasors.denyRenew = true
	okay, err = o.RunOnce(context.Background())
	if okay || err == nil || leasors.releases != 2 || stager.calls != 2 {
		t.Fatalf("expired claim staged an evaluation: %v %v %+v", okay, err, leasors)
	}
}


func TestLearningEvaluateCancellationStopsBeforeNextPhase(t *testing.T) {
	for _, phase := range []string{"claim", "source", "renew", "stage"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lease := LearningEvaluateLease{ExpansionID: uuid.New(), Token: uuid.New(),
				Until: time.Now().Add(time.Minute), TenantID: uuid.New(), RunID: uuid.New(),
				WorkflowID: uuid.New(), Candidate: v2input.Ref{ArtifactID: uuid.New(),
					SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}}
			task, err := v2input.TaskID(lease.WorkflowID, "learning.evaluate", 0)
			if err != nil { t.Fatal(err) }
			leases := &evaluationLeases{lease: lease, cancelAt: phase, cancel: cancel}
			source := evaluationSource{draft: TrainingDraftPrepared{
				TenantID: lease.TenantID, RunID: lease.RunID, WorkflowID: lease.WorkflowID}}
			stager := &evaluationStager{task: task}
			if phase == "source" { source.cancel = cancel }
			if phase == "stage" { stager.cancel = cancel }
			committer := &evaluationCommitter{}
			o := LearningEvaluateOrchestrator{Leases: leases, Source: source, Stager: stager,
				TransportForLease: func(LearningEvaluateLease) v2input.Publisher {
					return evaluationTransport{}
				}, Committer: committer}
			okay, err := o.RunOnce(ctx)
			if okay || !errors.Is(err, context.Canceled) || leases.releases != 1 || committer.calls != 0 {
				t.Fatalf("cancelled phase advanced: okay=%v err=%v leases=%+v commits=%d", okay, err, leases, committer.calls)
			}
			wantRenews, wantStages := 0, 0
			if phase == "renew" || phase == "stage" { wantRenews = 1 }
			if phase == "stage" { wantStages = 1 }
			if leases.renews != wantRenews || stager.calls != wantStages {
				t.Fatalf("later phase invoked: renews=%d stages=%d", leases.renews, stager.calls)
			}
		})
	}
}
