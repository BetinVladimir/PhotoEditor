package delivery

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

type prepareReadyTx struct {
	pgx.Tx
	calls int
}

func (tx *prepareReadyTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	tx.calls++
	if !strings.Contains(query, "v30_delivery_prepare_transport_ready") {
		panic("unexpected registration during ready replay")
	}
	return proofRow{accepted: true}
}

func TestPrepareRegistrationReadyReplayAndRunBinding(t *testing.T) {
	i, session, catalog, items := createDocumentsFixture(t)
	docs, err := i.CompileDocuments(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := docs.Plan.FreezeTask(docs.Plan.Roots(), PrepareResourceLimits{RAMBytes: 8 << 30, ScratchBytes: 8 << 30, OutputBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := frozen.PublishInputs(context.Background(), bindingPublisher{})
	if err != nil {
		t.Fatal(err)
	}
	tx := &prepareReadyTx{}
	if err := frozen.RegisterInputsInTransaction(context.Background(), tx, i.tenant, i.identity.Run, graph); err != nil || tx.calls != 1 {
		t.Fatalf("ready replay: calls=%d err=%v", tx.calls, err)
	}
	tx.calls = 0
	if err := frozen.RegisterInputsInTransaction(context.Background(), tx, i.tenant, uuid.New(), graph); err == nil || tx.calls != 0 {
		t.Fatal("foreign run reached receipt")
	}
	graph.Built.SHA256 = digest([]byte("changed"))
	if err := frozen.RegisterInputsInTransaction(context.Background(), tx, i.tenant, i.identity.Run, graph); err == nil || tx.calls != 0 {
		t.Fatal("changed inventory reached receipt")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := frozen.RegisterInputsInTransaction(ctx, tx, i.tenant, i.identity.Run, v2input.GraphTask{}); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
}
