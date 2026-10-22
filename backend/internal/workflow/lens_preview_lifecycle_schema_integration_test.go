package workflow

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
)

// Requires the current migrated schema; historical migration text cannot prove
// retention or purge safety of the deployed function after later replacements.
func TestLensPreviewLifecycleInstalledSchema(t *testing.T) {
 dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
 if dsn == "" { t.Skip("isolated fully migrated PostgreSQL required") }
 ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
 defer cancel()
 db, err := pgxpool.New(ctx, dsn)
 if err != nil { t.Fatal(err) }
 defer db.Close()
 var gc, purge, constraint string
 var deferred bool
 err = db.QueryRow(ctx, `SELECT
 pg_get_functiondef('photo.v2_artifact_has_references(uuid,uuid)'::regprocedure),
 pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure),
 (SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='photo.runs'::regclass AND conname='runs_v25_lens_preview_kind'),
 (SELECT condeferrable AND condeferred FROM pg_constraint WHERE conrelid='photo.runs'::regclass AND conname='runs_v25_lens_preview_source_fk')`).Scan(&gc, &purge, &constraint, &deferred)
 if err != nil { t.Fatal(err) }
 for _, required := range []string{"d.v25_lens_preview_params IS NOT NULL", "r.v25_lens_preview_task_id IS NOT NULL", "r.status = 'draft'", "r.cancel_requested"} {
  // SQL function source retains its authored formatting; normalize spacing.
  normalized := strings.Join(strings.Fields(gc), " ")
  compact := strings.ReplaceAll(normalized, " = ", "=")
  if !strings.Contains(compact, strings.ReplaceAll(required, " = ", "=")) { t.Fatalf("Lens draft GC lost %s", required) }
 }
 for _, column := range []string{"v26_derivative_task_id", "v29_perspective_preview_task_id", "v29_perspective_quality_task_id", "v29_multi_preview_task_id", "v29_multi_quality_task_id", "v28_restoration_preview_task_id"} {
  if !strings.Contains(constraint, column+" IS NULL") { t.Fatalf("mixed preview identity permitted: %s", column) }
 }
 if !deferred || !strings.Contains(strings.ToUpper(purge), "SET CONSTRAINTS ALL DEFERRED") || !strings.Contains(strings.ToUpper(purge), "SET CONSTRAINTS ALL IMMEDIATE") {
  t.Fatal("Lens runs-to-tasks link can obstruct tenant purge")
 }
}
