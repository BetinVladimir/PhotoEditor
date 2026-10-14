package workflow

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationCapacityNodeDrainGeneration(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	node := "drain-" + uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO photo.resource_nodes
	 (node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes)
	 VALUES($1,true,1024,0,1024)`, node)
	if err != nil {
		t.Fatal(err)
	}
	drainer := SQLNodeDrainer{DB: tx}
	first, err := drainer.Begin(ctx, node)
	if err != nil || first != 1 {
		t.Fatal("begin drain", first, err)
	}
	second, err := drainer.Begin(ctx, node)
	if err != nil || second != first {
		t.Fatal("idempotent begin", second, err)
	}
	status, err := drainer.Status(ctx, node, first)
	if err != nil || !status.ReadyToStop || status.ActiveReservations != 0 || status.OccupiedSlots != 0 {
		t.Fatal("empty node did not drain", status, err)
	}
	if _, err = tx.Exec(ctx, "UPDATE photo.resource_nodes SET enabled=true WHERE node_id=$1", node); err != nil {
		t.Fatal(err)
	}
	// A new registration cycle must invalidate the old stop proof.
	if _, err = tx.Exec(ctx, "SAVEPOINT stale_drain_probe"); err != nil {
		t.Fatal(err)
	}
	if _, err = drainer.Status(ctx, node, first); err == nil {
		t.Fatal("reenabled node accepted old drain proof")
	}
	if _, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT stale_drain_probe"); err != nil {
		t.Fatal(err)
	}
	third, err := drainer.Begin(ctx, node)
	if err != nil || third != first+1 {
		t.Fatal("new drain generation", third, err)
	}
	if _, err = tx.Exec(ctx, "SAVEPOINT old_generation_probe"); err != nil {
		t.Fatal(err)
	}
	if _, err = drainer.Status(ctx, node, first); err == nil {
		t.Fatal("old generation accepted after new drain")
	}
	if _, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT old_generation_probe"); err != nil {
		t.Fatal(err)
	}
	status, err = drainer.Status(ctx, node, third)
	if err != nil || !status.ReadyToStop {
		t.Fatal("new generation not ready", status, err)
	}
}
