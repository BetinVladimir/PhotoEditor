package v2draft

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
	"time"
)

func TestLocalPortraitSQLInventoryDigestParity(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PHOTO_TEST_DATABASE_URL with owner-role access and migration 00835 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("cannot connect to integration database")
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
		t.Fatal(err)
	}
	task := uuid.New()
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	entries := []v2input.Entry{{Ref: params, Contract: "Params"}}
	for _, contract := range []string{"Params", "WorkingImage", "MaskRef", "RasterizedMask", "ManualMask", "MaskPixels", "NpyArray", "Artifact"} {
		entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, Contract: contract})
	}
	entries, err = uniqueTaskInputs(entries)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := canonical.Marshal(entries)
	for _, operation := range []string{"portrait.hair", "portrait.clothing"} {
		digest, _ := canonical.Hash(struct {
			Entries   []v2input.Entry `json:"entries"`
			Operation string          `json:"operation"`
			Task      uuid.UUID       `json:"task"`
			Version   string          `json:"version"`
		}{entries, operation, task, "1"})
		var ready bool
		if err = tx.QueryRow(ctx, "SELECT photo.v28_local_inventory_digest_ready($1,$2,$3,$4,$5,$6::jsonb)", task, params.ArtifactID, params.SHA256, operation, digest, string(raw)).Scan(&ready); err != nil || !ready {
			t.Fatal("SQL digest parity", err)
		}
		if err = tx.QueryRow(ctx, "SELECT photo.v28_local_inventory_digest_ready($1,$2,$3,$4,$5,$6::jsonb)", task, params.ArtifactID, params.SHA256, operation, strings.Repeat("f", 64), string(raw)).Scan(&ready); err != nil || ready {
			t.Fatal("changed digest accepted", err)
		}
	}
}
