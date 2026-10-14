package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
	"time"
)

// Physical parity uses the same fixtures as Go and worker, not a SQL-text mock.
func TestLocalPortraitSQLResourceParity(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PHOTO_TEST_DATABASE_URL required; migration 00831 must be applied")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/local-resource-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Width    int64                 `json:"width"`
		Height   int64                 `json:"height"`
		Expected LocalResourceEstimate `json:"expected"`
	}
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		var result []byte
		if err = tx.QueryRow(ctx, "SELECT photo.v28_local_portrait_resources($1,$2)", fixture.Width, fixture.Height).Scan(&result); err != nil {
			t.Fatal(err)
		}
		var actual LocalResourceEstimate
		if json.Unmarshal(result, &actual) != nil || actual != fixture.Expected {
			t.Fatalf("SQL parity mismatch: %dx%d", fixture.Width, fixture.Height)
		}
		profile, _ := canonical.Marshal(map[string]any{"backend": "cpu", "renderer_version": "v2.2", "portrait_runtime_sha": strings.Repeat("a", 64), "resource_estimate": fixture.Expected})
		var valid bool
		if err = tx.QueryRow(ctx, "SELECT photo.v28_local_portrait_profile_shape($1,$2,$3,$4::jsonb)", fixture.Width, fixture.Height, strings.Repeat("a", 64), string(profile)).Scan(&valid); err != nil || !valid {
			t.Fatal("exact profile rejected", err)
		}
		if err = tx.QueryRow(ctx, "SELECT photo.v28_local_portrait_profile_shape($1,$2,$3,$4::jsonb || '{\"extra\":true}'::jsonb)", fixture.Width, fixture.Height, strings.Repeat("a", 64), string(profile)).Scan(&valid); err != nil || valid {
			t.Fatal("extra profile accepted", err)
		}
	}
	for _, size := range [][2]int64{{0, 1}, {1, 0}, {-1, 1}, {200000001, 1}, {200000000, 2}, {9223372036854775807, 9223372036854775807}} {
		var absent bool
		if err = tx.QueryRow(ctx, "SELECT photo.v28_local_portrait_resources($1,$2) IS NULL", size[0], size[1]).Scan(&absent); err != nil || !absent {
			t.Fatal("invalid dimensions accepted", size, err)
		}
	}
}
