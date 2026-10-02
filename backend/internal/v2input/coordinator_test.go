package v2input

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type coordinatorStore struct {
	outcome string
	run     uuid.UUID
	ticket  JobTicket
	steps   []string
}

func (s *coordinatorStore) Begin(context.Context, DraftShell, string, string) (string, uuid.UUID, error) {
	s.steps = append(s.steps, "begin")
	return s.outcome, s.run, nil
}

func (s *coordinatorStore) Seal(_ context.Context, _, _ uuid.UUID, graph []GraphTask) error {
	s.steps = append(s.steps, "seal")
	if len(graph) != 1 || graph[0].Built.Count != 1 {
		return ErrInvalid
	}
	return nil
}

func (s *coordinatorStore) Ticket(context.Context, uuid.UUID, uuid.UUID, string, string) (JobTicket, error) {
	s.steps = append(s.steps, "ticket")
	return s.ticket, nil
}

type orderedPublisher struct {
	*memoryPublisher
	steps *[]string
}

func (p orderedPublisher) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (Ref, error) {
	*p.steps = append(*p.steps, "publish")
	return p.memoryPublisher.Publish(ctx, task, ordinal, contract, body)
}

func TestCoordinatorPublishesOnlyBetweenAdmissionTransactions(t *testing.T) {
	ctx := context.Background()
	tenant, actor := uuid.New(), uuid.New()
	run, _ := RunID(tenant, actor, "key")
	workflow, _ := WorkflowID(run)
	task, _ := TaskID(workflow, "preview", 0)
	shell := DraftShell{Tenant: tenant, Run: run, WorkflowID: workflow, Actor: actor,
		Tasks: []DraftTask{{ID: task, NodeKey: "preview:0"}}}
	entries := admissionEntries(1)
	source := TaskSource{ID: task, Open: func(context.Context) (Cursor, error) {
		return &entryCursor{entries: entries}, nil
	}}
	store := &coordinatorStore{outcome: "created", run: run,
		ticket: JobTicket{RunID: run, WorkflowID: shell.WorkflowID, State: "queued"}}
	publisher := orderedPublisher{memoryPublisher: &memoryPublisher{bodies: make(map[uuid.UUID][]byte)}, steps: &store.steps}
	got, err := (Coordinator{Store: store, Publisher: publisher}).Admit(ctx, shell, "key", "hash", []TaskSource{source})
	if err != nil || got != store.ticket {
		t.Fatalf("admission failed: %+v %v", got, err)
	}
	if len(store.steps) != 5 || store.steps[0] != "begin" || store.steps[1] != "publish" ||
		store.steps[2] != "publish" || store.steps[3] != "seal" || store.steps[4] != "ticket" {
		t.Fatalf("object publication entered DB transaction: %v", store.steps)
	}
	store.steps = nil
	store.outcome = "replayed"
	if _, err := (Coordinator{Store: store, Publisher: publisher}).Admit(ctx, shell, "key", "hash", []TaskSource{source}); err != nil {
		t.Fatal(err)
	}
	if len(store.steps) != 2 || store.steps[0] != "begin" || store.steps[1] != "ticket" {
		t.Fatalf("replay republished inputs: %v", store.steps)
	}
	store.steps = nil
	store.outcome = "conflict"
	if _, err := (Coordinator{Store: store, Publisher: publisher}).Admit(ctx, shell, "key", "hash", []TaskSource{source}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict accepted: %v", err)
	}
}
