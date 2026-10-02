package v2input

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type admissionCall struct {
	query string
	args  []any
}

type admissionTx struct {
	pgx.Tx
	calls     []admissionCall
	sealGraph bool
}

type admissionRow struct {
	value any
}

func (r admissionRow) Scan(targets ...any) error {
	if len(targets) != 1 {
		return ErrInvalid
	}
	switch target := targets[0].(type) {
	case *int:
		*target = r.value.(int)
	case *bool:
		*target = r.value.(bool)
	default:
		return ErrInvalid
	}
	return nil
}

func (tx *admissionTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	tx.calls = append(tx.calls, admissionCall{query: query, args: args})
	if strings.Contains(query, "register_v2_task_input_batch") {
		var entries []Entry
		if err := json.Unmarshal(args[4].(json.RawMessage), &entries); err != nil {
			panic(err)
		}
		return admissionRow{len(entries)}
	}
	if strings.Contains(query, "seal_v2_graph") {
		return admissionRow{tx.sealGraph}
	}
	return admissionRow{true}
}

func admissionEntries(n int) []Entry {
	entries := make([]Entry, n)
	for i := range entries {
		id := uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012x", i+1))
		entries[i] = Entry{Ref: Ref{ArtifactID: id,
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Contract: "image/tiff"}
	}
	return entries
}

func TestAdmissionWritesPagedRowsThenSealsMatchingDigest(t *testing.T) {
	ctx := context.Background()
	task, tenant, run := uuid.New(), uuid.New(), uuid.New()
	entries := admissionEntries(513)
	p := &memoryPublisher{bodies: make(map[uuid.UUID][]byte)}
	built, err := BuildInputClosure(ctx, task, &entryCursor{entries: entries}, p)
	if err != nil {
		t.Fatal(err)
	}
	tx := &admissionTx{}
	open := func(context.Context) (Cursor, error) { return &entryCursor{entries: entries}, nil }
	if err := RegisterInTransaction(ctx, tx, tenant, run, task, built, open, []Ref{entries[0].Ref}); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 3 || tx.calls[0].args[3] != 0 || tx.calls[1].args[3] != 512 ||
		!strings.Contains(tx.calls[2].query, "seal_v2_task_inputs") {
		t.Fatalf("unexpected admission calls: %+v", tx.calls)
	}
	if tx.calls[2].args[5] != 513 || tx.calls[2].args[6] != built.SHA256 {
		t.Fatal("seal used a different inventory")
	}
}

func TestAdmissionRejectsChangedFrozenInputBeforeSeal(t *testing.T) {
	ctx := context.Background()
	task := uuid.New()
	entries := admissionEntries(2)
	built, err := BuildInputClosure(ctx, task, &entryCursor{entries: entries},
		&memoryPublisher{bodies: make(map[uuid.UUID][]byte)})
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]Entry(nil), entries...)
	changed[1].Ref.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	tx := &admissionTx{}
	open := func(context.Context) (Cursor, error) { return &entryCursor{entries: changed}, nil }
	if err := RegisterInTransaction(ctx, tx, uuid.New(), uuid.New(), task, built, open, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("changed input accepted: %v", err)
	}
	for _, call := range tx.calls {
		if strings.Contains(call.query, "seal_v2_task_inputs") {
			t.Fatal("changed input reached seal")
		}
	}
}

func TestGraphAdmissionSealsOnlyAfterAllTaskInputs(t *testing.T) {
	ctx := context.Background()
	tenant, run := uuid.New(), uuid.New()
	graph := make([]GraphTask, 2)
	for i := range graph {
		id := uuid.New()
		entries := admissionEntries(1)
		built, err := BuildInputClosure(ctx, id, &entryCursor{entries: entries},
			&memoryPublisher{bodies: make(map[uuid.UUID][]byte)})
		if err != nil {
			t.Fatal(err)
		}
		graph[i] = GraphTask{ID: id, Built: built,
			Open: func(context.Context) (Cursor, error) { return &entryCursor{entries: entries}, nil }}
	}
	tx := &admissionTx{sealGraph: true}
	if err := SealGraphInTransaction(ctx, tx, tenant, run, graph); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 5 || !strings.Contains(tx.calls[4].query, "seal_v2_graph") {
		t.Fatalf("graph sealed before all task inputs: %+v", tx.calls)
	}
	tx = &admissionTx{}
	if err := SealGraphInTransaction(ctx, tx, tenant, run, graph); !errors.Is(err, ErrInvalid) {
		t.Fatalf("failed graph seal accepted: %v", err)
	}
	if err := SealGraphInTransaction(ctx, &admissionTx{sealGraph: true}, tenant, run,
		[]GraphTask{graph[0], graph[0]}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate task accepted: %v", err)
	}
}
