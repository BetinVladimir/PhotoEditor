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

// Exercise the production predicate against isolated SQL relation fixtures.
// This covers canonical producer joins, not RLS or physical model bytes.
func TestIntegrationSigningHistoricalLearningProducerPredicates(t *testing.T) {
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
	query := signingHistoricalLearningBindingSQL
	for _, table := range []string{"learning_versions", "v2_workflows", "runs", "artifacts", "tasks", "task_results", "v2_task_inputs", "inbox", "snapshot_items", "task_edges"} {
		query = strings.ReplaceAll(query, "photo."+table, "fixture_"+table)
	}
	prefix := `WITH ids AS (
 SELECT $1::uuid tenant, $2::uuid project, $3::uuid version, $4::uuid model,
 $5::text sha, $6::uuid render, $7::text mode,
 '00000000-0000-0000-0000-000000000011'::uuid workflow,
 '00000000-0000-0000-0000-000000000012'::uuid run,
 '00000000-0000-0000-0000-000000000013'::uuid snapshot,
 '00000000-0000-0000-0000-000000000014'::uuid fit,
 '00000000-0000-0000-0000-000000000015'::uuid result,
 '00000000-0000-0000-0000-000000000016'::uuid training,
 '00000000-0000-0000-0000-000000000017'::uuid evaluation,
 '00000000-0000-0000-0000-000000000018'::uuid evaluated,
 '00000000-0000-0000-0000-000000000019'::uuid evaluated_result
 ), fixture_learning_versions AS (
 SELECT tenant tenant_id,project project_id,version id,workflow workflow_id,model model_artifact_id,training training_artifact_id,evaluation evaluation_artifact_id FROM ids
 ), fixture_v2_workflows AS (
 SELECT tenant tenant_id,project project_id,workflow id,run run_id,snapshot input_snapshot_id,
 CASE WHEN mode='workflow' THEN 'session' ELSE 'learning' END workflow_type,'v2.2'::text renderer_version FROM ids
 ), fixture_runs AS (
 SELECT tenant tenant_id,run id,snapshot input_snapshot_id,'v2'::text kind,mode<>'unsealed' graph_sealed, jsonb_build_object('v23_training',jsonb_build_object('manifest_sha',CASE WHEN mode='manifest_sha' THEN 'wrong' ELSE sha END)) config_snapshot FROM ids
 ), fixture_artifacts AS (
 SELECT tenant tenant_id,model id,sha sha256,fit created_by_task_id,1 produced_attempt,
 CASE WHEN mode='fence' THEN 2 ELSE 1 END::bigint produced_fence,
 'document'::text output_role,'ready'::text state,now() verified_at,'application/json'::text media_type,
 'version-1'::text object_version,NULL::timestamptz expires_at FROM ids
 UNION ALL SELECT tenant,training,sha,NULL::uuid,NULL::integer,NULL::bigint,'document','ready',now(),'application/json','training-version',NULL::timestamptz FROM ids
 UNION ALL SELECT tenant,evaluation,sha,evaluated,1,CASE WHEN mode='evaluation_fence' THEN 2 ELSE 1 END::bigint,
 'document','ready',now(),'application/json','evaluation-version',NULL::timestamptz FROM ids
 ), fixture_tasks AS (
 SELECT tenant tenant_id,fit id,run run_id,result canonical_result_id,1 attempt,1::bigint fencing_token,
 CASE WHEN mode='operation' THEN 'learning.evaluate' ELSE 'learning.fit' END operation,
 '1'::text operation_version,'succeeded'::text state FROM ids
 UNION ALL SELECT tenant,evaluated,run,evaluated_result,1,1::bigint,
 CASE WHEN mode='evaluation_operation' THEN 'learning.fit' ELSE 'learning.evaluate' END,'1','succeeded' FROM ids
 ), fixture_task_results AS (
 SELECT tenant tenant_id,fit task_id,result id,
 CASE WHEN mode='attempt' THEN 2 ELSE 1 END attempt,1::bigint fencing_token,
 CASE WHEN mode='output' THEN '[]'::jsonb ELSE jsonb_build_array(jsonb_build_object('artifact_id',model::text,'sha256',sha)) END outputs FROM ids
 UNION ALL SELECT tenant,evaluated,evaluated_result,1,1::bigint,
 CASE WHEN mode='evaluation_output' THEN '[]'::jsonb ELSE jsonb_build_array(jsonb_build_object('artifact_id',evaluation::text,'sha256',sha)) END FROM ids
 ), fixture_snapshot_items AS (
 SELECT tenant tenant_id,snapshot snapshot_id,0 ordinal,training artifact_id,
 CASE WHEN mode='snapshot_sha' THEN 'wrong' ELSE sha END sha256 FROM ids
 ), fixture_task_edges AS (
 SELECT tenant tenant_id,run run_id,fit prerequisite_id,evaluated dependent_id,'success'::text condition FROM ids WHERE mode<>'edge'
 ), fixture_v2_task_inputs AS (
 SELECT tenant tenant_id,project project_id,model artifact_id,sha sha256,render task_id,
 CASE WHEN mode='contract' THEN 'Artifact' ELSE 'CandidateProfile' END contract FROM ids
 ), fixture_inbox AS (
 SELECT tenant tenant_id,'v2-adopt'::text consumer,photo.v2_adoption_event_id(fit,result) event_id FROM ids WHERE mode<>'adoption'
 UNION ALL SELECT tenant,'v2-adopt',photo.v2_adoption_event_id(evaluated,evaluated_result) FROM ids WHERE mode<>'evaluation_adoption'
 ) `
	for _, mode := range []string{"valid", "workflow", "unsealed", "fence", "operation", "attempt", "output", "contract", "adoption", "manifest_sha", "snapshot_sha", "evaluation_fence", "evaluation_operation", "evaluation_output", "edge", "evaluation_adoption"} {
		t.Run(mode, func(t *testing.T) {
			var bound bool
			err := db.QueryRow(ctx, prefix+query, uuid.New(), uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64), uuid.New(), mode).Scan(&bound)
			if err != nil {
				t.Fatal(err)
			}
			if bound != (mode == "valid") {
				t.Fatal("unexpected historical binding", mode, bound)
			}
		})
	}
}
