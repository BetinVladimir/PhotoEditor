package provenance

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegrationSigningHistoricalStyleVersionBinding(t *testing.T) {
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
	query := strings.NewReplacer("photo.style_versions", "fixture_versions", "photo.styles", "fixture_styles").Replace(signingHistoricalStyleBindingSQL)
	prefix := `WITH ids AS(SELECT $1::uuid tenant,$2::uuid project,$3::uuid artifact,$4::text sha,$5::text mode,
 '00000000-0000-0000-0000-000000000011'::uuid style),
 fixture_styles AS(SELECT tenant tenant_id,style id,
 CASE WHEN mode='project' THEN style ELSE project END project_id,now() archived_at FROM ids),
 fixture_versions AS(SELECT tenant tenant_id,style style_id,
 CASE WHEN mode='artifact' THEN style ELSE artifact END export_artifact_id,
 CASE WHEN mode='sha' THEN 'wrong' ELSE sha END config_hash,
 CASE WHEN mode='pending' THEN 'pending' ELSE 'accepted' END approval_status,
 CASE WHEN mode='schema' THEN '1.0.0' ELSE '2.0.0' END schema_version,
 CASE WHEN mode='renderer' THEN 'other' ELSE 'photo_renderer_v2' END renderer_version FROM ids)
 `
	for _, mode := range []string{"valid_archived", "project", "artifact", "sha", "pending", "schema", "renderer"} {
		t.Run(mode, func(t *testing.T) {
			var bound bool
			err := db.QueryRow(ctx, prefix+query, uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64), mode).Scan(&bound)
			if err != nil {
				t.Fatal(err)
			}
			if bound != (mode == "valid_archived") {
				t.Fatal("wrong historical style binding", mode, bound)
			}
		})
	}
}
