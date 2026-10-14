package v2input

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"photoeditor/backend/internal/canonical"
)

type beginRow struct {
	outcome string
	run     pgtype.UUID
}

func (r beginRow) Scan(dest ...any) error {
	*dest[0].(*string) = r.outcome
	*dest[1].(*pgtype.UUID) = r.run
	return nil
}

type beginTx struct {
	pgx.Tx
	query string
	args  []any
	row   beginRow
}

func (tx *beginTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	tx.query, tx.args = query, args
	return tx.row
}

func TestDraftShellValidatesGraphBeforeDatabaseWrite(t *testing.T) {
	task := uuid.New()
	shell := DraftShell{
		Tenant: uuid.New(), Run: uuid.New(), WorkflowID: uuid.New(), Project: uuid.New(), Snapshot: uuid.New(), Actor: uuid.New(),
		Workflow: "preview", PolicySHA: strings.Repeat("a", 64),
		Bytes: 1024,
		Tasks: []DraftTask{{ID: task, NodeKey: "preview/0", Operation: "asset.inspect",
			OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: uuid.New(),
			ParamsSHA: strings.Repeat("c", 64), InputDigest: strings.Repeat("d", 64),
			ExecutionProfile: json.RawMessage(`{"backend":"cpu"}`)}},
	}
	shell.Config, _ = json.Marshal(map[string]string{"project_id": shell.Project.String()})
	shell.ConfigSHA, _ = canonical.Hash(json.RawMessage(shell.Config))
	tx := &admissionTx{}
	shell.ConfigSHA = strings.Repeat("b", 64)
	if err := CreateDraftShellInTransaction(context.Background(), tx, shell); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mismatched config digest accepted: %v", err)
	}
	if len(tx.calls) != 0 {
		t.Fatal("mismatched config reached database")
	}
	shell.ConfigSHA, _ = canonical.Hash(json.RawMessage(shell.Config))
	if err := CreateDraftShellInTransaction(context.Background(), tx, shell); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 1 || !strings.Contains(tx.calls[0].query, "create_v2_draft_shell") {
		t.Fatalf("unexpected shell call: %+v", tx.calls)
	}
	shell.Edges = []DraftEdge{{PrerequisiteID: task, DependentID: uuid.New()}}
	tx = &admissionTx{}
	if err := CreateDraftShellInTransaction(context.Background(), tx, shell); !errors.Is(err, ErrInvalid) {
		t.Fatalf("outside edge accepted: %v", err)
	}
	if len(tx.calls) != 0 {
		t.Fatal("invalid graph reached database")
	}
	shell.Edges = nil
	begin := &beginTx{row: beginRow{outcome: "pending", run: pgtype.UUID{Bytes: [16]byte(shell.Run), Valid: true}}}
	outcome, run, err := BeginAdmissionInTransaction(context.Background(), begin, shell,
		"same-key", strings.Repeat("e", 64))
	if err != nil || outcome != "pending" || run != shell.Run ||
		!strings.Contains(begin.query, "begin_v2_admission") || len(begin.args) != 15 {
		t.Fatalf("pending replay lost run: %s %s %v", outcome, run, err)
	}
	begin.query = ""
	if _, _, err := BeginAdmissionInTransaction(context.Background(), begin, shell,
		"bad key", strings.Repeat("e", 64)); !errors.Is(err, ErrInvalid) || begin.query != "" {
		t.Fatalf("invalid idempotency key reached database: %v", err)
	}
}

func TestSemanticShellQueueClassRefusesForeignRehashedConfig(t *testing.T){
 shell:=DraftShell{Tenant:uuid.New(),Run:uuid.New(),WorkflowID:uuid.New(),Project:uuid.New(),Snapshot:uuid.New(),Actor:uuid.New(),Workflow:"semantic",PolicySHA:strings.Repeat("a",64),Bytes:1024,
 Tasks:[]DraftTask{{ID:uuid.New(),ParamsArtifactID:uuid.New(),ParamsSHA:strings.Repeat("c",64),InputDigest:strings.Repeat("d",64),ExecutionProfile:json.RawMessage(`{"backend":"cpu"}`)}}}
 for _,queue:=range []string{"batch","interactive","training",""}{
  config:=map[string]any{"project_id":shell.Project.String(),"v27_semantic":map[string]any{}}
  if queue!=""{config["queue_class"]=queue}
  shell.Config,_=canonical.Marshal(config);shell.ConfigSHA,_=canonical.Hash(shell.Config)
  _,_,err:=encodeDraftShell(shell)
  if (queue=="batch"||queue=="")!=(err==nil){t.Fatal("wrong semantic shell queue result",queue,err)}
 }
}
