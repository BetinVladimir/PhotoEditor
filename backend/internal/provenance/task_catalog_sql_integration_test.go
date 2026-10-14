package provenance

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Planning exercises the actual recursive/LATERAL SQL and parameter types.
// It does not prove positive ancestry, ACL, revocation or concurrent lifecycle.
func TestIntegrationSigningTaskEvidenceQueryPlans(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated DB required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var plan []byte
	err = db.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+signingTaskEvidenceSQL, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()).Scan(&plan)
	if err != nil {
		t.Fatal("canonical ancestry query cannot be planned", err)
	}
	if !json.Valid(plan) {
		t.Fatal("missing SQL plan")
	}
}

func TestIntegrationSigningHistoricalLearningBindingQueryPlans(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated DB required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var plan []byte
	if err := db.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+signingHistoricalLearningBindingSQL, uuid.New(), uuid.New(), uuid.New(), uuid.New(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", uuid.New()).Scan(&plan); err != nil {
		t.Fatal("historical learning binding query cannot be planned", err)
	}
	if !json.Valid(plan) {
		t.Fatal("missing SQL plan")
	}
}

func TestIntegrationSigningHistoricalDescriptorBindingQueryPlans(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated DB required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var plan []byte
	if err := db.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+signingHistoricalDescriptorBindingSQL, uuid.New(), uuid.New(), uuid.New(), 0, uuid.New(), uuid.New(), sha, uuid.New(), sha, sha).Scan(&plan); err != nil {
		t.Fatal("historical descriptor binding query cannot be planned", err)
	}
	if !json.Valid(plan) {
		t.Fatal("missing SQL plan")
	}
}

func TestIntegrationSigningHistoricalBackgroundBindingQueryPlans(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated DB required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var plan []byte
	if err := db.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+signingHistoricalBackgroundBindingSQL, uuid.New(), uuid.New(), uuid.New(), uuid.New(), sha, uuid.New(), sha, "background", uuid.New(), uuid.New(), int64(100), "image/jpeg").Scan(&plan); err != nil {
		t.Fatal("historical background binding query cannot be planned", err)
	}
	if !json.Valid(plan) {
		t.Fatal("missing SQL plan")
	}
}

func TestIntegrationSigningHistoricalFrameBindingQueryPlans(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated DB required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var plan []byte
	if err := db.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+signingHistoricalFrameBindingSQL, uuid.New(), uuid.New(), uuid.New(), 0, uuid.New(), uuid.New(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Scan(&plan); err != nil {
		t.Fatal("historical frame binding query cannot be planned", err)
	}
	if !json.Valid(plan) {
		t.Fatal("missing SQL plan")
	}
}

// Evaluate the same role predicate embedded in recursive ancestry, including
// cross-operation role confusion. Full ancestry lifecycle fixtures remain required.
func TestIntegrationSigningCrossRunWorkingImageRoles(t *testing.T) {
 dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
 if dsn == "" { t.Skip("isolated migrated DB required") }
 ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
 defer cancel()
 db, err := pgxpool.New(ctx, dsn)
 if err != nil { t.Fatal(err) }
 defer db.Close()
 for _, tc := range []struct { operation, role string; want bool }{
  {"render.compose", "document", true},
  {"optical.negative", "working_image", true},
  {"render.compose", "working_image", false},
  {"optical.negative", "document", false},
  {"optical.negative", "mask", false},
  {"unknown", "working_image", false},
 } {
  t.Run(tc.operation+"/"+tc.role, func(t *testing.T) {
   var got bool
   query := "SELECT " + signingCrossRunWorkingImageRoleSQL + " FROM (SELECT $1::text AS operation) producer CROSS JOIN (SELECT $2::text AS output_role) input_artifact"
   if err := db.QueryRow(ctx, query, tc.operation, tc.role).Scan(&got); err != nil { t.Fatal(err) }
   if got != tc.want { t.Fatalf("role authorization = %v, want %v", got, tc.want) }
  })
 }
}
