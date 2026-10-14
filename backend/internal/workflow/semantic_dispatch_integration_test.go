package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

// Caller owns this isolated database, ready semantic child and enabled CPU node.
func TestIntegrationSemanticExposureDispatcher(t *testing.T) {
	dsn, raw := os.Getenv("PHOTO_TEST_DATABASE_URL"), os.Getenv("PHOTO_TEST_V27_DISPATCH_TASK")
	if dsn == "" || raw == "" {
		t.Skip("isolated semantic dispatch fixture required")
	}
	task, err := uuid.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	count, err := (Dispatcher{DB: pool}).Dispatch(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("dispatched=%d", count)
	}
	owner, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var state string
	var worker uuid.UUID
	var command []byte
	if err := owner.QueryRow(ctx, "SELECT state,dispatch_worker_id,dispatch_command FROM photo.tasks WHERE id=$1", task).Scan(&state, &worker, &command); err != nil {
		t.Fatal(err)
	}
	if state != "queued" || worker == uuid.Nil {
		t.Fatal("task not targeted")
	}
	var envelope map[string]any
	if json.Unmarshal(command, &envelope) != nil || envelope["task_id"] != task.String() {
		t.Fatal("command mismatch")
	}
	var outputs int
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.outbox WHERE event_id=$1 AND routing_key LIKE $2", envelope["event_id"], "worker."+worker.String()+".%").Scan(&outputs); err != nil {
		t.Fatal(err)
	}
	if outputs != 1 {
		t.Fatal("targeted command outbox missing")
	}
	t.Log("V27_DISPATCHER_OK")
}
