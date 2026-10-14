package v2input

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
	"time"
)

type assemblyStoreFake struct {
	lease                    SemanticAssemblyLease
	inputs                   []SemanticTransferResult
	renews, releases, stages int
	failRenew                int
	stageErr                 error
}

func (s *assemblyStoreFake) Claim(context.Context) (*SemanticAssemblyLease, error) {
	return &s.lease, nil
}
func (s *assemblyStoreFake) Inputs(context.Context, SemanticAssemblyLease) ([]SemanticTransferResult, error) {
	return s.inputs, nil
}
func (s *assemblyStoreFake) Renew(context.Context, SemanticAssemblyLease) (bool, error) {
	s.renews++
	return s.renews != s.failRenew, nil
}
func (s *assemblyStoreFake) Release(ctx context.Context, _ SemanticAssemblyLease) (bool, error) {
	s.releases++
	return ctx.Err() == nil, ctx.Err()
}
func (s *assemblyStoreFake) Stage(_ context.Context, l SemanticAssemblyLease, stage SemanticAssemblyStage) (uuid.UUID, error) {
	s.stages++
	if s.stageErr != nil {
		return uuid.Nil, s.stageErr
	}
	if len(stage.Transfers) != len(s.inputs) || stage.Transfers[0].Ordinal != 0 {
		return uuid.Nil, ErrInvalid
	}
	return TaskID(l.WorkflowID, "v2.semantic.assemble", 0)
}

type assemblySourceFake struct{ material SemanticAssemblyMaterial }

func (s assemblySourceFake) Load(_ context.Context, _ SemanticAssemblyLease, inputs []SemanticTransferResult) (SemanticAssemblyMaterial, error) {
	inputs[0].Ordinal = 999
	return s.material, nil
}

type assemblyCommitFake struct {
	calls int
	err   error
}

func (c *assemblyCommitFake) CommitSemanticAssembly(ctx context.Context, _, _, _, _ uuid.UUID, task GraphTask) error {
	c.calls++
	if c.err != nil {
		return c.err
	}
	_, err := semanticCommitInventory(ctx, task)
	return err
}

func TestSemanticAssemblyOrchestratesAndFencesFailedPhases(t *testing.T) {
	for _, mode := range []string{"success", "preflight", "renew", "stage", "transport", "commit"} {
		t.Run(mode, func(t *testing.T) {
			params, _, intent, snapshot, body, candidates, _ := semanticAssemblyFixture(t, 513)
			run := uuid.New()
			workflow, _ := WorkflowID(run)
			store := &assemblyStoreFake{lease: SemanticAssemblyLease{ExpansionID: uuid.New(), Token: uuid.New(), Until: time.Now().Add(time.Minute), TenantID: uuid.New(), RunID: run, WorkflowID: workflow}}
			for i, ref := range params.Candidates {
				task, _ := TaskID(workflow, "semantic.transfer", i)
				store.inputs = append(store.inputs, SemanticTransferResult{Ordinal: i, TaskID: task, Candidate: ref})
			}
			material := SemanticAssemblyMaterial{IntentRef: params.Intent, Intent: intent, Calibration: params.Calibration, ModelLockSHA: params.ModelLockSHA, Snapshot: snapshot, SnapshotBody: body, Candidates: candidates}
			docs := &scopedDocuments{}
			transport := &memoryPublisher{bodies: make(map[uuid.UUID][]byte)}
			commit := &assemblyCommitFake{}
			switch mode {
			case "preflight":
				material.Candidates[0].Body = []byte(`{}`)
			case "renew":
				store.failRenew = 1
			case "stage":
				store.stageErr = ErrInvalid
			case "transport":
				transport.bad = true
			case "commit":
				commit.err = ErrExpired
			}
			o := SemanticAssemblyOrchestrator{Store: store, Source: assemblySourceFake{material}, Documents: docs, TransportForLease: func(l SemanticAssemblyLease) Publisher {
				if l.Token != store.lease.Token {
					t.Fatal("transport fence changed")
				}
				return transport
			}, Committer: commit}
			processed, err := o.RunOnce(context.Background())
			if mode == "success" {
				if err != nil || !processed || store.releases != 0 || store.stages != 1 || commit.calls != 1 {
					t.Fatal(processed, err, store, commit)
				}
				if len(transport.bodies) < 2 {
					t.Fatal("transport not published")
				}
			} else {
				if err == nil || processed || store.releases != 1 {
					t.Fatal("failed phase not released", processed, err, store)
				}
				if mode != "commit" && commit.calls != 0 {
					t.Fatal("committed failed phase")
				}
			}
			if mode == "preflight" || mode == "renew" {
				if len(docs.scopes) != 0 {
					t.Fatal("params published before validation/authority")
				}
			} else {
				if len(docs.scopes) != 1 || docs.scopes[0].AssemblyLease != store.lease.Token || docs.scopes[0].Owner != store.lease.ExpansionID {
					t.Fatal("params publication lost lease scope")
				}
			}
			if mode == "renew" && !errors.Is(err, ErrExpired) {
				t.Fatal(err)
			}
		})
	}
}

type waitingAssemblySource struct{}

func (waitingAssemblySource) Load(ctx context.Context, _ SemanticAssemblyLease, _ []SemanticTransferResult) (SemanticAssemblyMaterial, error) {
	<-ctx.Done()
	return SemanticAssemblyMaterial{}, ctx.Err()
}

func TestSemanticAssemblyLeaseLossCancelsLongSourceResolution(t *testing.T) {
	run := uuid.New()
	workflow, _ := WorkflowID(run)
	task, _ := TaskID(workflow, "semantic.transfer", 0)
	store := &assemblyStoreFake{lease: SemanticAssemblyLease{ExpansionID: uuid.New(), Token: uuid.New(), Until: time.Now().Add(time.Minute), TenantID: uuid.New(), RunID: run, WorkflowID: workflow}, inputs: []SemanticTransferResult{{Ordinal: 0, TaskID: task, Candidate: Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}, failRenew: 2}
	docs := &scopedDocuments{}
	commit := &assemblyCommitFake{}
	o := SemanticAssemblyOrchestrator{Store: store, Source: waitingAssemblySource{}, Documents: docs, TransportForLease: func(SemanticAssemblyLease) Publisher { return &memoryPublisher{bodies: make(map[uuid.UUID][]byte)} }, Committer: commit, RenewEvery: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	processed, err := o.RunOnce(ctx)
	if processed || !errors.Is(err, ErrExpired) || store.releases != 1 || store.stages != 0 || commit.calls != 0 || len(docs.scopes) != 0 {
		t.Fatal("lease loss did not cancel before IO publication", processed, err, store, commit)
	}
}

func TestSemanticAssemblyWorkTimeoutStopsLongSourceResolution(t *testing.T) {
	run := uuid.New()
	workflow, _ := WorkflowID(run)
	task, _ := TaskID(workflow, "semantic.transfer", 0)
	store := &assemblyStoreFake{lease: SemanticAssemblyLease{ExpansionID: uuid.New(), Token: uuid.New(), Until: time.Now().Add(time.Minute), TenantID: uuid.New(), RunID: run, WorkflowID: workflow}, inputs: []SemanticTransferResult{{Ordinal: 0, TaskID: task, Candidate: Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}}
	docs := &scopedDocuments{}
	commit := &assemblyCommitFake{}
	orchestrator := SemanticAssemblyOrchestrator{Store: store, Source: waitingAssemblySource{}, Documents: docs,
		TransportForLease: func(SemanticAssemblyLease) Publisher { return &memoryPublisher{bodies: make(map[uuid.UUID][]byte)} },
		Committer:         commit, RenewEvery: time.Second, WorkTimeout: 20 * time.Millisecond}
	processed, err := orchestrator.RunOnce(context.Background())
	if processed || !errors.Is(err, context.DeadlineExceeded) || store.renews < 1 || store.releases != 1 || store.stages != 0 || commit.calls != 0 || len(docs.scopes) != 0 {
		t.Fatal("semantic assembly exceeded work budget", processed, err, store, commit)
	}
	orchestrator.WorkTimeout = 2*time.Hour + time.Nanosecond
	if processed, err = orchestrator.RunOnce(context.Background()); processed || !errors.Is(err, ErrInvalid) {
		t.Fatal("unbounded semantic work accepted", processed, err)
	}
}
