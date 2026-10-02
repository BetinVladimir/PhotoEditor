package v2learning

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationTrainingCandidateSource(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	parts := strings.Split(os.Getenv("PHOTO_TEST_LEARNING_IDS"), ",")
	if dsn == "" || len(parts) != 10 {
		t.Skip("isolated V23 training fixture required")
	}
	ids := make([]uuid.UUID, len(parts))
	for i, part := range parts {
		var err error
		ids[i], err = uuid.Parse(part)
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	tx, err := api.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	rows, err := LoadTrainingCandidates(ctx, tx, ids[0], ids[1], 1,
		ids[4:10], ids[2], ids[3])
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].ConsentLive || !rows[0].SourceReady ||
		!rows[0].BaseReady || !rows[0].ApprovedReady ||
		rows[0].ApprovalID == nil || rows[0].SessionID != ids[4] ||
		len(rows[0].ChangedPaths) != 1 || rows[0].ChangedPaths[0] != "/ev" {
		t.Fatalf("unexpected source inventory: %+v", rows)
	}
}
