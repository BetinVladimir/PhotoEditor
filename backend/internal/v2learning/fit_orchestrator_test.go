package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type fitOrchestratorCursor struct {
	entry v2input.Entry
	done  bool
}

func (c *fitOrchestratorCursor) Next(context.Context) (v2input.Entry, bool, error) {
	if c.done {
		return v2input.Entry{}, false, nil
	}
	c.done = true
	return c.entry, true, nil
}

type fitOrchestratorStager struct {
	steps *[]string
	stage TrainingStageResult
	fit   FitStageResult
}

func (s fitOrchestratorStager) Stage(context.Context,
	TrainingDraftPrepared) (TrainingStageResult, error) {
	*s.steps = append(*s.steps, "snapshot")
	return s.stage, nil
}

func (s fitOrchestratorStager) StageFitTask(context.Context,
	TrainingDraftPrepared, LearningPolicy) (FitStageResult, error) {
	*s.steps = append(*s.steps, "fit")
	return s.fit, nil
}

type fitOrchestratorPublisher struct{ steps *[]string }

func (p fitOrchestratorPublisher) Publish(_ context.Context, task uuid.UUID,
	ordinal int, contract string, body []byte) (v2input.Ref, error) {
	*p.steps = append(*p.steps, "publish")
	sum := sha256.Sum256(body)
	return v2input.Ref{ArtifactID: uuid.NewSHA1(task,
		[]byte(strconv.Itoa(ordinal)+contract)),
		SHA256: hex.EncodeToString(sum[:])}, nil
}

type fitOrchestratorCommitter struct{ steps *[]string }

func (c fitOrchestratorCommitter) CommitGraph(_ context.Context,
	_, _ uuid.UUID, graph []v2input.GraphTask) error {
	*c.steps = append(*c.steps, "seal")
	if len(graph) != 1 || graph[0].Built.Count != 1 ||
		graph[0].Open == nil {
		return ErrInvalidTrainingDraft
	}
	return nil
}

func TestFitOrchestratorPublishesBetweenSnapshotAndGraphCommit(t *testing.T) {
	project, profile, policyRequest, manifest := compiledTrainingDraftFixture(t)
	draft, err := PrepareTrainingDraft(uuid.New(), uuid.New(), uuid.New(),
		project, profile, "fit-key", 4096, policyRequest, manifest)
	if err != nil {
		t.Fatal(err)
	}
	task, err := v2input.TaskID(draft.WorkflowID, "learning.fit", 0)
	if err != nil {
		t.Fatal(err)
	}
	entry := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(),
		SHA256: strings.Repeat("a", 64)}, Contract: "TrainingManifest"}
	steps := []string{}
	stager := fitOrchestratorStager{steps: &steps,
		stage: TrainingStageResult{Outcome: "created",
			Snapshot: TrainingArtifactSnapshot{SnapshotID: draft.SnapshotID,
				SnapshotDigest: draft.SnapshotDigest}},
		fit: FitStageResult{TaskID: task,
			Closure: FitClosure{InputDigest: strings.Repeat("b", 64),
				Open: func(context.Context) (v2input.Cursor, error) {
					return &fitOrchestratorCursor{entry: entry}, nil
				}}}}
	orchestrator := TrainingFitOrchestrator{Stager: stager,
		Transport: fitOrchestratorPublisher{steps: &steps},
		Committer: fitOrchestratorCommitter{steps: &steps}}
	if _, err := orchestrator.Admit(context.Background(), draft,
		policyRequest.Policy); err != nil {
		t.Fatal(err)
	}
	if strings.Join(steps, ",") != "snapshot,fit,publish,publish,seal" {
		t.Fatalf("unsafe Fit admission order: %v", steps)
	}
	steps = nil
	stager.stage.Outcome = "replayed"
	orchestrator.Stager = stager
	if _, err := orchestrator.Admit(context.Background(), draft,
		policyRequest.Policy); err == nil || strings.Join(steps, ",") != "snapshot" {
		t.Fatal("replay republished Fit inputs")
	}
}
