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

func TestIntegrationSigningRenderTransportAncestryPredicate(t *testing.T) {
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
	predicate := strings.NewReplacer("photo.v2_task_inputs", "registered_inputs", "photo.v2_workflows", "consuming_workflows").Replace(signingCrossRunInputPredicateSQL)
	prefix := `WITH values_in AS(SELECT $1::uuid tenant,$2::uuid task,$3::uuid artifact,$4::text sha,$5::uuid project,$6::text mode),
 child AS(SELECT tenant tenant_id,task id,task run_id,
 CASE WHEN mode='operation' THEN 'other' ELSE 'render.compose' END operation,
 CASE WHEN mode='version' THEN '2' ELSE '1' END operation_version,
 CASE WHEN mode='direct' THEN jsonb_build_array(jsonb_build_object('artifact_id',artifact::text,'sha256',sha)) ELSE '[]'::jsonb END inputs FROM values_in),
 input_artifact AS(SELECT artifact id,sha sha256 FROM values_in),
 registered_inputs AS(SELECT tenant tenant_id,task task_id,project project_id,artifact artifact_id,
 CASE WHEN mode='sha' THEN 'wrong' ELSE sha END sha256,
 CASE WHEN mode='contract' THEN 'Artifact' ELSE 'WorkingImage' END contract FROM values_in WHERE mode<>'missing'),
 consuming_workflows AS(SELECT tenant tenant_id,project project_id,task run_id FROM values_in WHERE mode<>'workflow')
 SELECT `
	for _, mode := range []string{"valid", "direct", "operation", "version", "sha", "contract", "missing", "workflow"} {
		t.Run(mode, func(t *testing.T) {
			var allowed bool
			err := db.QueryRow(ctx, prefix+predicate+` FROM child CROSS JOIN input_artifact`, uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64), uuid.New(), mode).Scan(&allowed)
			if err != nil {
				t.Fatal(err)
			}
			if allowed != (mode == "valid" || mode == "direct") {
				t.Fatal("incorrect transport ancestry", mode, allowed)
			}
		})
	}
}

func TestIntegrationSigningAdoptedRenderChildAncestryPredicate(t *testing.T) {
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
	predicate := strings.NewReplacer("photo.v2_workflows", "render_workflows", "photo.v2_artifact_links", "adoption_links", "photo.artifacts", "root_artifacts").Replace(signingCrossRunProducerOutputPredicateSQL)
	prefix := `WITH ids AS(SELECT $1::uuid tenant,$2::uuid task,$3::uuid artifact,$4::text sha,$5::uuid project,$6::text mode,
 '00000000-0000-0000-0000-000000000091'::uuid root),
 producer AS(SELECT tenant tenant_id,task id,task run_id,1 attempt,1::bigint fencing_token,
 'render.compose'::text operation,CASE WHEN mode='version' THEN '2' ELSE '1' END operation_version,mode='cancelled' cancel_requested FROM ids),
 input_artifact AS(SELECT artifact id,sha sha256,'document'::text output_role FROM ids),
 version AS(SELECT CASE WHEN mode='pending' THEN 'pending' ELSE 'accepted' END review_state FROM ids),
 proof AS(SELECT jsonb_build_array(jsonb_build_object('artifact_id',CASE WHEN mode='direct' THEN artifact ELSE root END::text,'sha256',sha)) outputs FROM ids),
 render_workflows AS(SELECT tenant tenant_id,task id,task run_id,project project_id,'v2.2'::text renderer_version FROM ids),
 adoption_links AS(
 SELECT tenant tenant_id,project project_id,task workflow_id,artifact artifact_id,sha sha256,1 ordinal,
 encode(sha256(convert_to('tasks/'||task||'/$/working_image','UTF8')),'hex') purpose FROM ids WHERE mode<>'missing_child'
 UNION ALL SELECT tenant,project,task,root,sha,0,encode(sha256(convert_to('tasks/'||task||'/$','UTF8')),'hex') FROM ids WHERE mode<>'missing_root'
 ),root_artifacts AS(
 SELECT tenant tenant_id,root id,sha sha256,task created_by_task_id,1 produced_attempt,
 CASE WHEN mode='fence' THEN 2 ELSE 1 END::bigint produced_fence,'document'::text output_role,
 'application/json'::text media_type,100::bigint bytes,'ready'::text state,now() verified_at,
 CASE WHEN mode='object_version' THEN 'null' ELSE 'v1' END object_version,NULL::timestamptz expires_at FROM ids
 ) SELECT `
	for _, mode := range []string{"valid", "direct", "version", "cancelled", "pending", "missing_child", "missing_root", "fence", "object_version"} {
		t.Run(mode, func(t *testing.T) {
			var allowed bool
			err := db.QueryRow(ctx, prefix+predicate+` FROM producer CROSS JOIN input_artifact CROSS JOIN version CROSS JOIN proof`, uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64), uuid.New(), mode).Scan(&allowed)
			if err != nil {
				t.Fatal(err)
			}
			if allowed != (mode == "valid" || mode == "direct") {
				t.Fatal("incorrect adopted child ancestry", mode, allowed)
			}
		})
	}
}
