package v2input

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakePlanStore struct {
	lease      SessionPlanLease
	results    []SessionShardResult
	stageError error
	renews     int
	releases   int
	staged     bool
}

func (s *fakePlanStore) Claim(context.Context) (*SessionPlanLease, error) {
	return &s.lease, nil
}
func (s *fakePlanStore) Inputs(context.Context, SessionPlanLease) ([]SessionShardResult, error) {
	return s.results, nil
}
func (s *fakePlanStore) Renew(context.Context, SessionPlanLease) (bool, error) {
	s.renews++
	return true, nil
}
func (s *fakePlanStore) Release(context.Context, SessionPlanLease) (bool, error) {
	s.releases++
	return true, nil
}
func (s *fakePlanStore) Stage(_ context.Context, lease SessionPlanLease,
	stage SessionPlanStage) (uuid.UUID, error) {
	if s.stageError != nil {
		return uuid.Nil, s.stageError
	}
	if !validRef(stage.Params) || len(stage.Shards) != len(s.results) {
		return uuid.Nil, ErrInvalid
	}
	s.staged = true
	return TaskID(lease.WorkflowID, "session.plan", 0)
}

type fakePlanSource struct {
	context Ref
	count   int
}

func (s fakePlanSource) Load(context.Context, SessionPlanLease) (FrozenSessionPlan, error) {
	return FrozenSessionPlan{ItemCount: s.count, Context: s.context,
		Policy: map[string]any{"schema_version": 1}}, nil
}
func (s fakePlanSource) Closure(_ context.Context, _ SessionPlanLease,
	results []SessionShardResult, params Ref) (SessionPlanClosure, error) {
	entries := []Entry{{Ref: params, Contract: "Params"},
		{Ref: s.context, Contract: "SessionPlanContext"}}
	for _, item := range results {
		entries = append(entries, Entry{Ref: item.Document, Contract: "DescriptorShard"})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String()
	})
	return SessionPlanClosure{Open: func(context.Context) (Cursor, error) {
		return &entryCursor{entries: entries}, nil
	}, InputDigest: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		Profile: json.RawMessage(`{"backend":"cpu","renderer_version":"v2.2"}`)}, nil
}

type fakePlanCommitter struct{ count int }

func (c *fakePlanCommitter) CommitSessionPlan(_ context.Context, _, _, _, _ uuid.UUID,
	task GraphTask) error {
	if task.ID == uuid.Nil || task.Built.Count != 3 || task.Open == nil {
		return ErrInvalid
	}
	c.count++
	return nil
}

func TestSessionPlanOrchestratorPublishesStagesAndCloses(t *testing.T) {
	workflow := uuid.New()
	descriptors, _ := CompileSessionDescriptors(workflow, 1)
	store := &fakePlanStore{lease: SessionPlanLease{ExpansionID: uuid.New(),
		Token: uuid.New(), Until: time.Now().Add(time.Minute), TenantID: uuid.New(),
		RunID: uuid.New(), WorkflowID: workflow},
		results: []SessionShardResult{{TaskID: descriptors.Tasks[0].ID,
			Document: Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}}
	docs := &scopedDocuments{}
	committer := &fakePlanCommitter{}
	transportCalls := 0
	orchestrator := SessionPlanOrchestrator{Store: store,
		Source: fakePlanSource{count: 1, context: Ref{ArtifactID: uuid.New(),
			SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}},
		Documents: docs, TransportForLease: func(lease SessionPlanLease) Publisher {
			if lease.TenantID != store.lease.TenantID || lease.RunID != store.lease.RunID {
				t.Fatal("transport scoped to wrong run")
			}
			transportCalls++
			return &memoryPublisher{bodies: map[uuid.UUID][]byte{}}
		},
		Committer: committer}
	completed, err := orchestrator.RunOnce(context.Background())
	if err != nil || !completed || !store.staged || store.renews != 3 ||
		store.releases != 0 || committer.count != 1 || len(docs.scopes) != 1 || transportCalls != 1 {
		t.Fatalf("incomplete orchestration: %v %v %+v", completed, err, store)
	}
	store.stageError = errors.New("stage failed")
	completed, err = orchestrator.RunOnce(context.Background())
	if completed || err == nil || store.releases != 1 || committer.count != 1 {
		t.Fatalf("failure did not release lease: %v %v %+v", completed, err, store)
	}
}
