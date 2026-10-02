package workflow

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestIntegrationRenderBatchPassFinalizesAndSynchronizes(t *testing.T) {
	dsn, terminal, queued := os.Getenv("PHOTO_TEST_DATABASE_URL"), os.Getenv("PHOTO_BATCH_TERMINAL"), os.Getenv("PHOTO_BATCH_QUEUED")
	if dsn == "" || terminal == "" || queued == "" {
		t.Skip("isolated committed render-batch fixtures required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	processor := RenderBatchFinalizer{DB: pool}
	changed, err := processor.ProcessOne(ctx)
	if err != nil || !changed {
		t.Fatal("combined pass", changed, err)
	}
	for id, want := range map[string]string{terminal: "failed", queued: "queued"} {
		var status string
		var revision int64
		if err := admin.QueryRow(ctx, "SELECT status,revision FROM photo.runs WHERE id=$1", id).Scan(&status, &revision); err != nil || status != want || revision != 2 {
			t.Fatal("durable aggregate", want, status, revision, err)
		}
		var events int
		if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.run_events WHERE run_id=$1 AND type=$2", id, "run."+want).Scan(&events); err != nil || events != 1 {
			t.Fatal("event", events, err)
		}
	}
	changed, err = processor.ProcessOne(ctx)
	if err != nil || changed {
		t.Fatal("idle replay", changed, err)
	}
}
