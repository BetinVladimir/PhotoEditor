package v2capture

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"math"
	"os"
	"testing"
	"time"
)

// The same versioned vectors exercise the worker and PostgreSQL implementations.
func TestIntegrationTechnicalQualityVectors(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	raw, err := os.ReadFile("testdata/technical-quality.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name      string          `json:"name"`
		Requested []string        `json:"requested"`
		Checks    json.RawMessage `json:"checks"`
		Quality   *float64        `json:"quality"`
		Error     bool            `json:"error"`
	}
	if err = json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			report, _ := json.Marshal(map[string]any{"checks": v.Checks})
			policy, _ := json.Marshal(map[string]any{"checks": v.Requested})
			var got *float64
			err := db.QueryRow(ctx, `SELECT photo.capture_technical_quality($1::jsonb,$2::jsonb)`, string(report), string(policy)).Scan(&got)
			if v.Error {
				if err == nil {
					t.Fatal("invalid evidence accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (got == nil) != (v.Quality == nil) {
				t.Fatalf("null mismatch got=%v want=%v", got, v.Quality)
			}
			if got != nil && math.Abs(*got-*v.Quality) > 1e-12 {
				t.Fatalf("got=%v want=%v", *got, *v.Quality)
			}
		})
	}
}
