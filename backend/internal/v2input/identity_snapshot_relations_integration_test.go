package v2input

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Requires an isolated database with migrations through 02152 applied by the
// test harness. No product data is written by this shape-only test.
func TestIntegrationIdentitySnapshotSQLRelations(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_fn_owner"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	asset, face, person, consent := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	base := fmt.Sprintf(`{"confirmed_faces":[{"asset_version_id":"%s","consent_id":"%s","epoch":"3","face_id":"%s","person_id":"%s"}],"grants":[{"consent_id":"%s","epoch":"3","person_id":"%s","purpose":"identity_matching"}],"schema_version":1}`,
		asset, consent, face, person, consent, person)
	for name, tc := range map[string]struct {
		body string
		want bool
	}{
		"matching":       {base, true},
		"wrong person":   {strings.Replace(base, `"person_id":"`+person.String()+`"}],"grants"`, `"person_id":"`+uuid.NewString()+`"}],"grants"`, 1), false},
		"missing grant":  {strings.Replace(base, `"grants":[{"consent_id":"`+consent.String()+`","epoch":"3","person_id":"`+person.String()+`","purpose":"identity_matching"}]`, `"grants":[]`, 1), false},
		"duplicate face": {strings.Replace(base, `}],"grants"`, `},{"asset_version_id":"`+asset.String()+`","consent_id":"`+consent.String()+`","epoch":"3","face_id":"`+face.String()+`","person_id":"`+person.String()+`"}],"grants"`, 1), false},
		"extra key":      {strings.Replace(base, `"schema_version":1}`, `"schema_version":1,"forged":true}`, 1), false},
	} {
		t.Run(name, func(t *testing.T) {
			var got bool
			if err := pool.QueryRow(ctx, `SELECT photo.v23_identity_snapshot_relations_ready($1)`, tc.body).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("SQL relation gate: got %t, want %t", got, tc.want)
			}
		})
	}
}
