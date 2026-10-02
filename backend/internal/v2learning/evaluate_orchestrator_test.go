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
}

func (s *evaluationLeases) Claim(context.Context) (*LearningEvaluateLease, error) {
	return &s.lease, nil
}
func (s *evaluationLeases) Renew(context.Context, LearningEvaluateLease) (bool, error) {
	s.renews++
	return !s.denyRenew, nil
}
func (s *evaluationLeases) Release(context.Context, LearningEvaluateLease) (bool, error) {
	s.releases++
	return true, nil
}

type evaluationSource struct{ draft TrainingDraftPrepared }

func (s evaluationSource) Load(context.Context, LearningEvaluateLease) (TrainingDraftPrepared, []LearningRenderInputs, error) {
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
}

func (s *evaluationStager) StageEvaluateTask(_ context.Context,
	lease LearningEvaluateLease, draft TrainingDraftPrepared,
	_ []LearningRenderInputs) (EvaluateStageResult, error) {
	s.calls++
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
