package delivery

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"strings"
	"testing"
)

// Uses only an isolated migrated test database. No migrations are applied here.
func TestDeliveryStorageDeniesDirectRuntimeAccess(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("database configuration failed")
	}
	defer db.Close()
	for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler", "photo_gateway", "photo_maintenance"} {
		for _, table := range []string{"delivery_packages", "delivery_items", "delivery_signing_closures"} {
			t.Run(role+"/"+table, func(t *testing.T) {
				tx, err := db.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+role); err != nil {
					t.Fatal(err)
				}
				_, err = tx.Exec(ctx, "SELECT tenant_id FROM photo."+table+" LIMIT 1")
				var pgerr *pgconn.PgError
				if !errors.As(err, &pgerr) || pgerr.Code != "42501" {
					t.Fatalf("direct runtime read must be denied, got %v", err)
				}
			})
		}
	}
	for _, table := range []string{"delivery_packages", "delivery_items", "delivery_signing_closures"} {
		var enabled, forced bool
		if err := db.QueryRow(ctx, "SELECT relrowsecurity,relforcerowsecurity FROM pg_catalog.pg_class WHERE oid=to_regclass($1)", "photo."+table).Scan(&enabled, &forced); err != nil {
			t.Fatal(err)
		}
		if !enabled || !forced {
			t.Fatal("delivery RLS must be forced", table)
		}
	}
}

func TestDeliveryDownloadTargetRequiresCurrentPublishedScope(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	signature := "photo.v30_delivery_download_target(uuid,uuid,uuid,uuid,text,integer,uuid,text)"
	var api, worker, scheduler, portal, public bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_api',$1,'EXECUTE'),
	 has_function_privilege('photo_worker',$1,'EXECUTE'),
	 has_function_privilege('photo_scheduler',$1,'EXECUTE'),
	 has_function_privilege('photo_portal',$1,'EXECUTE'),
	 EXISTS(SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(p.proacl) a
	  WHERE p.oid=$1::regprocedure AND a.grantee=0 AND a.privilege_type='EXECUTE')`,
		signature).Scan(&api, &worker, &scheduler, &portal, &public)
	if err != nil || !api || worker || scheduler || portal || public {
		t.Fatal("Delivery target ACL drift", err)
	}
	var definition string
	if err := db.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`, signature).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	policyGate := strings.Index(definition, "photo.v31_signing_policy_ready")
	closureGate := strings.Index(definition, "FROM photo.delivery_signing_closures")
	manifestBranch := strings.Index(definition, "IF p_kind='manifest' THEN")
	if policyGate < 0 || closureGate < policyGate || manifestBranch < closureGate {
		t.Fatal("required signing policy and closure must gate manifest and slot download")
	}
	for _, required := range []string{"signer_task.canonical_result_id=pm.adopted_result_id",
		"a.produced_attempt=signer_result.attempt", "a.produced_fence=signer_result.fencing_token",
		"signer_workflow.workflow_type='provenance'",
		"export_row.preset_hash IS DISTINCT FROM item.preset_sha",
		"slot_index:=p_ordinal*preset_count+preset_position",
		"export_run.id=photo.v30_uuid5(r.id,'child:export:'||slot_index::text)",
		"export_run.config_snapshot->'v30_delivery_export'->>'parent_run_id'=r.id::text",
		"export_row.id=photo.v30_uuid5(export_run.id,'v30:delivery-export:1')",
		"export_result.id=export_task.canonical_result_id",
		"encoded.id=export_row.artifact_id",
		"encoded.produced_attempt=export_result.attempt",
		"encoded.produced_fence=export_result.fencing_token",
		"adopted.event_id=photo.v2_adoption_event_id("} {
		if !strings.Contains(definition, required) {
			t.Fatal("signed download causal authority missing", required)
		}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenantID.String()); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `SELECT artifact_id FROM photo.v30_delivery_download_target($1,$2,$3,$4,'slot',0,$5,'photo')`,
		tenantID, uuid.New(), uuid.New(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() || rows.Err() != nil {
		t.Fatal("fabricated Delivery slot became downloadable", rows.Err())
	}
}

func TestDeliveryDownloadIntentCannotFreezeFabricatedScope(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	signature := "photo.create_v30_delivery_download_intent(uuid,uuid,uuid,uuid,text,integer,uuid,text,uuid,bytea,timestamptz)"
	var api, scheduler, worker, portal bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_api',$1,'EXECUTE'),
	 has_function_privilege('photo_scheduler',$1,'EXECUTE'),
	 has_function_privilege('photo_worker',$1,'EXECUTE'),
	 has_function_privilege('photo_portal',$1,'EXECUTE')`, signature).Scan(&api, &scheduler, &worker, &portal)
	if err != nil || !api || scheduler || worker || portal {
		t.Fatal("Delivery intent ACL drift", err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenantID.String()); err != nil {
		t.Fatal(err)
	}
	var created bool
	err = tx.QueryRow(ctx, `SELECT photo.create_v30_delivery_download_intent($1,$2,$3,$4,'manifest',NULL,NULL,'photo',$5,$6,clock_timestamp()+interval '30 seconds')`,
		tenantID, uuid.New(), uuid.New(), uuid.New(), uuid.New(), []byte(strings.Repeat("x", 32))).Scan(&created)
	if err != nil || created {
		t.Fatal("fabricated Delivery intent created", err)
	}
}

func TestDeliveryDownloadIntentNullHashFailsClosed(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var definition string
	err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.create_v30_delivery_download_intent(uuid,uuid,uuid,uuid,text,integer,uuid,text,uuid,bytea,timestamptz)'::regprocedure)`).Scan(&definition)
	if err != nil || !strings.Contains(definition, "p_token_hash IS NULL OR octet_length(p_token_hash)<>32") {
		t.Fatal("download intent NULL hash gate missing", err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenantID.String()); err != nil {
		t.Fatal(err)
	}
	var created bool
	err = tx.QueryRow(ctx, `SELECT photo.create_v30_delivery_download_intent($1,$2,$3,$4,'manifest',NULL,NULL,'photo',$5,NULL,clock_timestamp()+interval '30 seconds')`,
		tenantID, uuid.New(), uuid.New(), uuid.New(), uuid.New()).Scan(&created)
	if err != nil || created {
		t.Fatal("NULL hash did not return normal refusal", err, created)
	}
}

func TestDeliveryIntentRedemptionRetainsLiveTargetAndObjectVersion(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var definition string
	err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.redeem_download_intent(bytea,text)'::regprocedure)`).Scan(&definition)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"i.intent_kind='delivery'", "v30_delivery_download_target",
		"i.delivery_session_id", "i.delivery_sha", "i.delivery_object_version", "consumed_at=clock_timestamp()"} {
		if !strings.Contains(definition, required) {
			t.Fatal("Delivery redemption authority missing", required)
		}
	}
	if strings.Contains(definition, "l.permissions->'download'<>'true'::jsonb") ||
		!strings.Contains(definition, "l.permissions->'download' IS DISTINCT FROM 'true'::jsonb") ||
		!strings.Contains(definition, "jsonb_typeof(l.allowed_presets) IS DISTINCT FROM 'array'") ||
		!strings.Contains(definition, "jsonb_array_elements(CASE WHEN jsonb_typeof(l.allowed_presets)='array'") ||
		!strings.Contains(definition, "s.collection_id IS DISTINCT FROM j.collection_id") {
		t.Fatal("legacy download redemption must fail closed on absent permission or mismatched guest collection")
	}
	var outcome string
	err = db.QueryRow(ctx, `SELECT outcome FROM photo.redeem_download_intent($1,'photo')`,
		[]byte(strings.Repeat("x", 32))).Scan(&outcome)
	if err != nil || outcome != "invalid" {
		t.Fatal("fabricated Delivery bearer accepted", err, outcome)
	}
}

func TestDeliveryDownloadPostSigningAuthorityRejectsUnknownBearer(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	signature := "photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)"
	var api, portal, worker, scheduler bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_api',$1,'EXECUTE'),
	 has_function_privilege('photo_portal',$1,'EXECUTE'),
	 has_function_privilege('photo_worker',$1,'EXECUTE'),
	 has_function_privilege('photo_scheduler',$1,'EXECUTE')`, signature).
		Scan(&api, &portal, &worker, &scheduler)
	if err != nil || !api || !portal || worker || scheduler {
		t.Fatal("post-signing ACL drift", err)
	}
	var current bool
	err = db.QueryRow(ctx, `SELECT photo.v30_delivery_download_intent_current($1,'photo','missing','v1',$2,1,'image/jpeg')`,
		[]byte(strings.Repeat("x", 32)), strings.Repeat("a", 64)).Scan(&current)
	if err != nil || current {
		t.Fatal("unknown bearer passed post-signing check", err)
	}
	var definition string
	err = db.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`, signature).Scan(&definition)
	if err != nil || strings.Contains(definition, "IF i.intent_kind IN('item','volume') THEN RETURN true") ||
		!strings.Contains(definition, "a.object_key IS NOT DISTINCT FROM p_object_key") ||
		!strings.Contains(definition, "a.object_version IS NOT DISTINCT FROM p_object_version") ||
		!strings.Contains(definition, "a.sha256 IS NOT DISTINCT FROM p_sha") ||
		!strings.Contains(definition, "a.bytes IS NOT DISTINCT FROM p_bytes") ||
		!strings.Contains(definition, "a.media_type IS NOT DISTINCT FROM p_media_type") {
		t.Fatal("legacy download post-signing object pin missing", err)
	}
	for _, required := range []string{"photo.share_sessions", "photo.share_links",
		"photo.publications", "photo.asset_versions", "photo.export_presets",
		"photo.download_volumes", "photo.download_jobs", "photo.memberships",
		"photo.project_acl", "photo.snapshot_items",
		"s.collection_id IS DISTINCT FROM j.collection_id",
		"jsonb_array_elements(CASE WHEN jsonb_typeof(l.allowed_presets)='array'"} {
		if !strings.Contains(definition, required) {
			t.Fatal("legacy download post-signing policy recheck missing", required)
		}
	}
}

func TestDeliverySigningClosurePrivateRetentionAndPurgeACL(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var owner string
	var secure, schedulerRetain, schedulerRead, apiRetain, apiRead, workerRetain, workerRead, ownerDelete bool
	err = db.QueryRow(ctx, `SELECT pg_get_userbyid(p.proowner),p.prosecdef,
	 has_function_privilege('photo_scheduler','photo.retain_v30_delivery_signing_closure(uuid,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_scheduler','photo.read_v30_delivery_signing_closure(uuid,uuid)','EXECUTE'),
	 has_function_privilege('photo_api','photo.retain_v30_delivery_signing_closure(uuid,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.read_v30_delivery_signing_closure(uuid,uuid)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.retain_v30_delivery_signing_closure(uuid,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.read_v30_delivery_signing_closure(uuid,uuid)','EXECUTE'),
	 has_table_privilege('photo_fn_owner','photo.delivery_signing_closures','DELETE')
	 FROM pg_proc p WHERE p.oid='photo.retain_v30_delivery_signing_closure(uuid,uuid,text,text)'::regprocedure`).Scan(
		&owner, &secure, &schedulerRetain, &schedulerRead, &apiRetain, &apiRead, &workerRetain, &workerRead, &ownerDelete)
	if err != nil {
		t.Fatal(err)
	}
	if owner != "photo_fn_owner" || !secure || !schedulerRetain || !schedulerRead || apiRetain || apiRead || workerRetain || workerRead || !ownerDelete {
		t.Fatal("signing closure owner, role or purge privilege drift")
	}
	var publicRetain, publicRead bool
	err = db.QueryRow(ctx, `SELECT
	 EXISTS(SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(p.proacl) a
	  WHERE p.oid='photo.retain_v30_delivery_signing_closure(uuid,uuid,text,text)'::regprocedure
	  AND a.grantee=0 AND a.privilege_type='EXECUTE'),
	 EXISTS(SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(p.proacl) a
	  WHERE p.oid='photo.read_v30_delivery_signing_closure(uuid,uuid)'::regprocedure
	  AND a.grantee=0 AND a.privilege_type='EXECUTE')`).Scan(&publicRetain, &publicRead)
	if err != nil {
		t.Fatal(err)
	}
	if publicRetain || publicRead {
		t.Fatal("PUBLIC signing closure access")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	var retained bool
	err = tx.QueryRow(ctx, `SELECT photo.retain_v30_delivery_signing_closure($1,$2,$3,$4)`,
		uuid.New(), uuid.New(), digest([]byte("{}")), "{}").Scan(&retained)
	if err != nil || retained {
		t.Fatal("fabricated signing closure lease accepted", err)
	}
	var body, sha string
	err = tx.QueryRow(ctx, `SELECT closure_jcs,closure_sha FROM photo.read_v30_delivery_signing_closure($1,$2)`,
		uuid.New(), uuid.New()).Scan(&body, &sha)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("fabricated signing closure read accepted", err)
	}
	var statusAllowed, apiStatus, workerStatus bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.read_v30_delivery_signer_status(uuid,uuid,integer,uuid)','EXECUTE'),
	 has_function_privilege('photo_api','photo.read_v30_delivery_signer_status(uuid,uuid,integer,uuid)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.read_v30_delivery_signer_status(uuid,uuid,integer,uuid)','EXECUTE')`).Scan(&statusAllowed, &apiStatus, &workerStatus)
	if err != nil || !statusAllowed || apiStatus || workerStatus {
		t.Fatal("signer status ACL drift", err)
	}
	for _, entry := range []struct {
		signature string
		required  []string
	}{
		{"photo.retain_v30_delivery_signing_closure(uuid,uuid,text,text)", []string{"frozen_input_jcs", "signing_policy_id", "signing_policy_sha", "SigningPolicy", "signing:policy:", "QualifiedExport", "provenance:artifact-snapshot:1", "renderer_version", "config_snapshot", "graph_sealed", "node_key"}},
		{"photo.read_v30_delivery_signing_closure(uuid,uuid)", []string{"frozen_input_jcs", "signing_policy_id", "signing_policy_sha"}},
		{"photo.read_v30_delivery_signer_status(uuid,uuid,integer,uuid)", []string{"frozen_input_jcs", "signing_policy_id", "signing_policy_sha", "SigningPolicy", "signing:policy:", "QualifiedExport", "blocked"}},
		{"photo.v30_delivery_signing_assembly_gate(uuid,uuid)", []string{"frozen_input_jcs", "signing_policy_id", "signing_policy_sha", "retain_v30_delivery_signing_closure"}},
		{"photo.activate_v30_delivery_assembly(uuid,uuid,jsonb,text)", []string{"v30_delivery_signing_assembly_gate"}},
		{"photo.v30_delivery_shard_scope(uuid,uuid,text,text)", []string{"v30_delivery_signing_assembly_gate", "v30_delivery_export_observation"}},
		{"photo.v30_delivery_signed_document_scope(uuid,uuid,text,text)", []string{"v30_delivery_signing_assembly_gate", "v30_delivery_saved_assembly", "delivery_signing_closures", "v30_delivery_signed_closure", "v30_delivery_signed_envelope"}},
		{"photo.reserve_v30_delivery_signed_document(uuid,uuid,text,text,text,uuid,uuid,uuid)", []string{"v30_delivery_signed_document_scope", "tenant_usage", "backend_documents", "graph_expansions"}},
		{"photo.complete_v30_delivery_signed_document(uuid,uuid,text,text,text,uuid,uuid,text,uuid,uuid,uuid)", []string{"v30_delivery_signed_document_scope", "tenant_usage", "backend_documents", "artifacts", "graph_expansions"}},
		{"photo.read_v30_delivery_signed_envelope(uuid,uuid,uuid,text)", []string{"v30_delivery_signed_document_scope", "backend_documents", "artifacts", "v30_delivery_signed_envelope"}},
		{"photo.v30_delivery_assembly_params_scope(uuid,uuid,text)", []string{"v30_delivery_saved_assembly", "retain_v30_delivery_assembly", "read_v30_delivery_signed_envelope"}},
		{"photo.v30_delivery_signed_assembly_transport_ready(uuid,uuid,jsonb,text)", []string{"read_v30_delivery_signed_envelope", "SignedDeliveryAssemblyEnvelope", "SignedDeliveryAssemblyClosure", "SignedExport", "C2PAManifest", "v2_input_closure_ready", "v30_delivery_assembly_inventory", "v30_delivery_assembly_resources_ready", "actual_inventory", "expected_inventory"}},
		{"photo.v30_delivery_signed_runtime_documents(uuid,uuid,uuid,uuid,text,text)", []string{"v30_delivery_signed_ready_document", "delivery_signing_closures", "v31_signing_policy_ready", "SignedDeliveryAssemblyEnvelope", "SignedDeliveryAssemblyClosure", "v30_delivery_proof"}},
		{"photo.v30_delivery_signed_current_receipts(uuid,uuid,uuid,uuid,text,text)", []string{"v30_delivery_signed_runtime_documents", "QualifiedExport", "SigningPolicy", "provenance:artifact-snapshot:1", "signed_export", "application/c2pa", "validation_sha", "state NOT IN('ready','pending','omitted')", "state IN('pending','omitted') AND required"}},
		{"photo.v30_delivery_signed_current_matrix(uuid,uuid,uuid,uuid,text,text)", []string{"v30_delivery_signed_runtime_documents", "v30_delivery_signed_current_receipts", "v30_delivery_current_export_observation", "v30_delivery_signed_ready_document", "v30_delivery_assembly_resources_ready"}},
		{"photo.v30_delivery_signed_source_scope(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)", []string{"v30_delivery_lease_ready", "v30_delivery_signed_current_matrix", "v30_delivery_signed_runtime_documents", "actor_id", "parent_id"}},
		{"photo.v30_delivery_signed_adoption_scope(uuid,uuid,integer,bigint,uuid,text)", []string{"v2_adoption_context", "v30_delivery_signed_current_matrix", "v30_delivery_signed_runtime_documents", "params_sha", "actor_id", "parent_id"}},
		{"photo.v30_delivery_signed_manifest_authority(uuid,uuid,integer,bigint,uuid,text,text)", []string{"v30_delivery_signed_adoption_scope", "v30_delivery_signed_runtime_documents", "v30_delivery_signed_ready_document", "envelope_sha", "signing_closure", "created_by_task_id", "produced_fence"}},
		{"photo.adopt_v30_delivery_signed_assembled(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text)", []string{"v30_delivery_signed_manifest_authority", "adopt_v2_plan_pre_v30_assembly", "delivery_items", "delivery_packages", "delivery.assembled"}},
		{"photo.activate_v30_delivery_signed_assembly(uuid,uuid,jsonb,text)", []string{"v30_delivery_signed_assembly_transport_ready", "v30_delivery_signed_current_matrix", "activate_bound_task", "v30-assembly-activated"}},
		{"photo.v30_delivery_signed_activation_objects(uuid,uuid,uuid,uuid,text,text)", []string{"v30_delivery_signed_current_matrix", "v30_delivery_signed_runtime_documents", "closure_jcs", "object_version", "signed_export", "application/c2pa"}},
		{"photo.v30_delivery_signer_delegation_scope(uuid,uuid,integer,uuid)", []string{"read_v30_delivery_signer_status", "v31_signing_policy_ready", "export_artifact_id", "idempotency_key", "workflow_id"}},
		{"photo.v31_delivery_lineage_scope(uuid,uuid,integer,uuid,jsonb,text)", []string{"v30_delivery_signer_delegation_scope", "asset_versions", "task_results", "object_version", "export_artifact_id", "download_allowed"}},
		{"photo.reserve_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text)", []string{"v31_delivery_lineage_scope", "tenant_usage", "v31_signing_lineage", "reserved_bytes", "cleanup_after"}},
		{"photo.complete_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text,uuid,text)", []string{"v31_delivery_lineage_scope", "DOCUMENT_QUOTA_ACCOUNTING", "v31_signing_lineage", "object_version", "reserved_bytes"}},
		{"photo.v31_delivery_export_evidence(uuid,uuid,integer,uuid)", []string{"v30_delivery_signer_delegation_scope", "v31_delivery_lineage_scope", "object_version", "export_artifact_id", "project_id"}},
		{"photo.v31_delivery_final_task_evidence(uuid,uuid,integer,uuid)", []string{"v31_delivery_export_evidence", "canonical_result_id", "fencing_token", "input_digest", "result_digest", "artifact_id", "sha256"}},
		{"photo.v31_delivery_same_run_task_evidence(uuid,uuid,integer,uuid,uuid)", []string{"v31_delivery_export_evidence", "task_edges", "canonical_result_id", "fencing_token", "LIMIT 5001"}},
		{"photo.v31_delivery_ancestor_task_evidence(uuid,uuid,integer,uuid,uuid)", []string{"v31_delivery_export_evidence", "task_edges", "asset_versions", "WorkingImage", "review_state='accepted'", "root_artifact", "LIMIT 5001"}},
		{"photo.v31_delivery_task_document(uuid,uuid,integer,uuid,uuid,uuid,text,integer,text)", []string{"v31_delivery_ancestor_task_evidence", "v31_delivery_export_evidence", "InputClosureChunk", "working_image", "produced_fence", "object_version"}},
		{"photo.v31_delivery_direct_producer(uuid,uuid,integer,uuid,uuid,text)", []string{"v31_delivery_export_evidence", "v31_delivery_ancestor_task_evidence", "canonical_result_id", "produced_fence", "outputs"}},
		{"photo.v31_delivery_adopted_image_producer(uuid,uuid,integer,uuid,uuid,text)", []string{"v31_delivery_export_evidence", "v31_delivery_ancestor_task_evidence", "working_image", "root_artifact", "produced_fence", "v2_artifact_links"}},
		{"photo.v31_delivery_original_source(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,text)", []string{"v31_delivery_export_evidence", "v31_delivery_ancestor_task_evidence", "upload_state='verified'", "master_artifact_id", "created_by_run_id", "produced_fence"}},
		{"photo.v31_delivery_candidate_review(uuid,uuid,integer,uuid,uuid,uuid,text)", []string{"v31_delivery_export_evidence", "v31_delivery_adopted_image_producer", "v31_delivery_ancestor_task_evidence", "decision_hash", "candidate_artifact_id", "LIMIT 2"}},
	} {
		var definition string
		if err := db.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`, entry.signature).Scan(&definition); err != nil {
			t.Fatal(err)
		}
		for _, required := range entry.required {
			if !strings.Contains(definition, required) {
				t.Fatal("signing policy authority absent", entry.signature, required)
			}
		}
	}
	var observedState string
	err = tx.QueryRow(ctx, `SELECT signing_status FROM photo.read_v30_delivery_signer_status($1,$2,$3,$4)`,
		uuid.New(), uuid.New(), 0, uuid.New()).Scan(&observedState)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("fabricated signer status accepted", err)
	}
	var signingGate, apiGate, workerGate bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v30_delivery_signing_assembly_gate(uuid,uuid)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v30_delivery_signing_assembly_gate(uuid,uuid)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v30_delivery_signing_assembly_gate(uuid,uuid)','EXECUTE')`).Scan(&signingGate, &apiGate, &workerGate)
	if err != nil || !signingGate || apiGate || workerGate {
		t.Fatal("signed assembly gate ACL drift", err)
	}
	var privateScope bool
	err = db.QueryRow(ctx, `SELECT has_function_privilege('photo_scheduler','photo.v30_delivery_signed_document_scope(uuid,uuid,text,text)','EXECUTE')`).Scan(&privateScope)
	if err != nil || privateScope {
		t.Fatal("signed document scope is not private", err)
	}
	for _, signature := range []string{
		"photo.reserve_v30_delivery_signed_document(uuid,uuid,text,text,text,uuid,uuid,uuid)",
		"photo.complete_v30_delivery_signed_document(uuid,uuid,text,text,text,uuid,uuid,text,uuid,uuid,uuid)",
	} {
		var owner string
		var secure, scheduler, api, worker, gateway, public bool
		err = db.QueryRow(ctx, `SELECT pg_get_userbyid(p.proowner),p.prosecdef,
		 has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),
		 has_function_privilege('photo_api',p.oid,'EXECUTE'),
		 has_function_privilege('photo_worker',p.oid,'EXECUTE'),
		 has_function_privilege('photo_gateway',p.oid,'EXECUTE'),
		 EXISTS(SELECT 1 FROM aclexplode(p.proacl) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
		 FROM pg_proc p WHERE p.oid=$1::regprocedure`, signature).Scan(&owner, &secure, &scheduler, &api, &worker, &gateway, &public)
		if err != nil || owner != "photo_fn_owner" || !secure || !scheduler || api || worker || gateway || public {
			t.Fatal("signed document publication ACL drift", signature, err)
		}
	}
	var reserveOutcome string
	var reserveID, reserveToken *uuid.UUID
	var reserveReady bool
	err = tx.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_delivery_signed_document($1,$2,$3,$4,$5,$6,$7,$8)`,
		uuid.New(), uuid.New(), "v30_delivery_signed_closure", "{}", "documents", uuid.New(), uuid.New(), uuid.New()).Scan(&reserveOutcome, &reserveID, &reserveToken, &reserveReady)
	if err != nil || reserveOutcome != "forbidden" || reserveID != nil || reserveToken != nil || reserveReady {
		t.Fatal("fabricated signed reserve accepted", err)
	}
	var completed bool
	err = tx.QueryRow(ctx, `SELECT photo.complete_v30_delivery_signed_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		uuid.New(), uuid.New(), "v30_delivery_signed_closure", "{}", "documents", uuid.New(), uuid.New(), "version-1", uuid.New(), uuid.New(), uuid.New()).Scan(&completed)
	if err != nil || completed {
		t.Fatal("fabricated signed complete accepted", err)
	}
	var envelopeBody, envelopeSHA string
	err = tx.QueryRow(ctx, `SELECT envelope_jcs,envelope_sha FROM photo.read_v30_delivery_signed_envelope($1,$2,$3,$4)`,
		uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64)).Scan(&envelopeBody, &envelopeSHA)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("fabricated signed envelope read accepted", err)
	}
	// Reuse the already-declared ACL result variables for this distinct reader.
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.read_v30_delivery_signed_envelope(uuid,uuid,uuid,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.read_v30_delivery_signed_envelope(uuid,uuid,uuid,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.read_v30_delivery_signed_envelope(uuid,uuid,uuid,text)','EXECUTE'),
	 EXISTS(SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(p.proacl) a
	  WHERE p.oid='photo.read_v30_delivery_signed_envelope(uuid,uuid,uuid,text)'::regprocedure
	  AND a.grantee=0 AND a.privilege_type='EXECUTE')`).Scan(&schedulerRead, &apiRead, &workerRead, &publicRead)
	if err != nil || !schedulerRead || apiRead || workerRead || publicRead {
		t.Fatal("signed envelope reader ACL drift", err)
	}
	var signedTransport bool
	err = tx.QueryRow(ctx, `SELECT photo.v30_delivery_signed_assembly_transport_ready($1,$2,$3::jsonb,$4)`,
		uuid.New(), uuid.New(), `{}`, `{}`).Scan(&signedTransport)
	if err != nil || signedTransport {
		t.Fatal("fabricated signed assembly transport accepted", err)
	}
	var schedulerTransport, apiTransport, workerTransport bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v30_delivery_signed_assembly_transport_ready(uuid,uuid,jsonb,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v30_delivery_signed_assembly_transport_ready(uuid,uuid,jsonb,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v30_delivery_signed_assembly_transport_ready(uuid,uuid,jsonb,text)','EXECUTE')`).Scan(&schedulerTransport, &apiTransport, &workerTransport)
	if err != nil || !schedulerTransport || apiTransport || workerTransport {
		t.Fatal("signed transport ACL drift", err)
	}
	var runtimeRoot *string
	err = db.QueryRow(ctx, `SELECT photo.v30_delivery_signed_runtime_documents($1,$2,$3,$4,$5,$6)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64), `{}`).Scan(&runtimeRoot)
	if err != nil || runtimeRoot != nil {
		t.Fatal("fabricated signed runtime documents accepted", err)
	}
	var schedulerRuntime, apiRuntime, workerRuntime bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v30_delivery_signed_runtime_documents(uuid,uuid,uuid,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v30_delivery_signed_runtime_documents(uuid,uuid,uuid,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v30_delivery_signed_runtime_documents(uuid,uuid,uuid,uuid,text,text)','EXECUTE')`).Scan(&schedulerRuntime, &apiRuntime, &workerRuntime)
	if err != nil || schedulerRuntime || apiRuntime || workerRuntime {
		t.Fatal("signed runtime documents became public", err)
	}
	var currentReceipts bool
	err = db.QueryRow(ctx, `SELECT photo.v30_delivery_signed_current_receipts($1,$2,$3,$4,$5,$6)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64), `{}`).Scan(&currentReceipts)
	if err != nil || currentReceipts {
		t.Fatal("fabricated signed current receipts accepted", err)
	}
	var currentMatrix bool
	err = db.QueryRow(ctx, `SELECT photo.v30_delivery_signed_current_matrix($1,$2,$3,$4,$5,$6)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64), `{}`).Scan(&currentMatrix)
	if err != nil || currentMatrix {
		t.Fatal("fabricated signed current matrix accepted", err)
	}
	var signedSource *string
	err = db.QueryRow(ctx, `SELECT photo.v30_delivery_signed_source_scope($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		uuid.New(), uuid.New(), uuid.New(), 1, int64(1), uuid.New(), uuid.New(), strings.Repeat("a", 64), `{}`, strings.Repeat("b", 64)).Scan(&signedSource)
	if err != nil || signedSource != nil {
		t.Fatal("fabricated signed worker scope accepted", err)
	}
	var workerSignedScope, apiSignedScope, schedulerSignedScope bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_worker','photo.v30_delivery_signed_source_scope(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v30_delivery_signed_source_scope(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)','EXECUTE'),
	 has_function_privilege('photo_scheduler','photo.v30_delivery_signed_source_scope(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)','EXECUTE')`).Scan(&workerSignedScope, &apiSignedScope, &schedulerSignedScope)
	if err != nil || !workerSignedScope || apiSignedScope || schedulerSignedScope {
		t.Fatal("signed worker source ACL drift", err)
	}
	var signedAdoption *string
	err = db.QueryRow(ctx, `SELECT photo.v30_delivery_signed_adoption_scope($1,$2,$3,$4,$5,$6)`,
		uuid.New(), uuid.New(), 1, int64(1), uuid.New(), `{}`).Scan(&signedAdoption)
	if err != nil || signedAdoption != nil {
		t.Fatal("fabricated signed adoption scope accepted", err)
	}
	var schedulerAdoption, apiAdoption, workerAdoption bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v30_delivery_signed_adoption_scope(uuid,uuid,integer,bigint,uuid,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v30_delivery_signed_adoption_scope(uuid,uuid,integer,bigint,uuid,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v30_delivery_signed_adoption_scope(uuid,uuid,integer,bigint,uuid,text)','EXECUTE')`).Scan(&schedulerAdoption, &apiAdoption, &workerAdoption)
	if err != nil || !schedulerAdoption || apiAdoption || workerAdoption {
		t.Fatal("signed adoption scope ACL drift", err)
	}
	var signedManifest *string
	err = db.QueryRow(ctx, `SELECT photo.v30_delivery_signed_manifest_authority($1,$2,$3,$4,$5,$6,$7)`,
		uuid.New(), uuid.New(), 1, int64(1), uuid.New(), `{}`, `{}`).Scan(&signedManifest)
	if err != nil || signedManifest != nil {
		t.Fatal("fabricated signed manifest authority accepted", err)
	}
	var schedulerManifest, apiManifest, workerManifest bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v30_delivery_signed_manifest_authority(uuid,uuid,integer,bigint,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v30_delivery_signed_manifest_authority(uuid,uuid,integer,bigint,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v30_delivery_signed_manifest_authority(uuid,uuid,integer,bigint,uuid,text,text)','EXECUTE')`).Scan(&schedulerManifest, &apiManifest, &workerManifest)
	if err != nil || schedulerManifest || apiManifest || workerManifest {
		t.Fatal("signed manifest authority ACL drift", err)
	}
	var schedulerSignedCommit, apiSignedCommit, workerSignedCommit bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.adopt_v30_delivery_signed_assembled(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.adopt_v30_delivery_signed_assembled(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.adopt_v30_delivery_signed_assembled(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text)','EXECUTE')`).Scan(&schedulerSignedCommit, &apiSignedCommit, &workerSignedCommit)
	if err != nil || !schedulerSignedCommit || apiSignedCommit || workerSignedCommit {
		t.Fatal("signed commit ACL drift", err)
	}
	var committedSigned bool
	err = db.QueryRow(ctx, `SELECT photo.adopt_v30_delivery_signed_assembled($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		uuid.New(), uuid.New(), 1, int64(1), uuid.New(), `[]`, strings.Repeat("a", 64), `{}`, `{}`).Scan(&committedSigned)
	if err != nil || committedSigned {
		t.Fatal("fabricated signed package commit accepted", err)
	}
	var activatedSigned bool
	err = db.QueryRow(ctx, `SELECT photo.activate_v30_delivery_signed_assembly($1,$2,$3,$4)`,
		uuid.New(), uuid.New(), `{}`, `{}`).Scan(&activatedSigned)
	if err != nil || activatedSigned {
		t.Fatal("fabricated signed assembly activation accepted", err)
	}
	var schedulerActivation, apiActivation, workerActivation bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.activate_v30_delivery_signed_assembly(uuid,uuid,jsonb,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.activate_v30_delivery_signed_assembly(uuid,uuid,jsonb,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.activate_v30_delivery_signed_assembly(uuid,uuid,jsonb,text)','EXECUTE')`).Scan(&schedulerActivation, &apiActivation, &workerActivation)
	if err != nil || !schedulerActivation || apiActivation || workerActivation {
		t.Fatal("signed activation ACL drift", err)
	}
	var activationObjects int
	err = db.QueryRow(ctx, `SELECT count(*) FROM photo.v30_delivery_signed_activation_objects($1,$2,$3,$4,$5,$6)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64), `{}`).Scan(&activationObjects)
	if err != nil || activationObjects != 0 {
		t.Fatal("fabricated signed activation object inventory accepted", err)
	}
	var schedulerObjects, apiObjects, workerObjects bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v30_delivery_signed_activation_objects(uuid,uuid,uuid,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v30_delivery_signed_activation_objects(uuid,uuid,uuid,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v30_delivery_signed_activation_objects(uuid,uuid,uuid,uuid,text,text)','EXECUTE')`).Scan(&schedulerObjects, &apiObjects, &workerObjects)
	if err != nil || !schedulerObjects || apiObjects || workerObjects {
		t.Fatal("signed activation inventory ACL drift", err)
	}
	var delegation *string
	err = db.QueryRow(ctx, `SELECT photo.v30_delivery_signer_delegation_scope($1,$2,$3,$4)`,
		uuid.New(), uuid.New(), 0, uuid.New()).Scan(&delegation)
	if err != nil || delegation != nil {
		t.Fatal("fabricated signer delegation accepted", err)
	}
	var schedulerDelegation, apiDelegation, workerDelegation bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v30_delivery_signer_delegation_scope(uuid,uuid,integer,uuid)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v30_delivery_signer_delegation_scope(uuid,uuid,integer,uuid)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v30_delivery_signer_delegation_scope(uuid,uuid,integer,uuid)','EXECUTE')`).Scan(&schedulerDelegation, &apiDelegation, &workerDelegation)
	if err != nil || !schedulerDelegation || apiDelegation || workerDelegation {
		t.Fatal("signer delegation ACL drift", err)
	}
	var delegatedLineage *uuid.UUID
	err = db.QueryRow(ctx, `SELECT photo.v31_delivery_lineage_scope($1,$2,$3,$4,$5,$6)`,
		uuid.New(), uuid.New(), 0, uuid.New(), `{}`, `photos`).Scan(&delegatedLineage)
	if err != nil || delegatedLineage != nil {
		t.Fatal("fabricated delegated lineage accepted", err)
	}
	var schedulerLineage, apiLineage, workerLineage bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v31_delivery_lineage_scope(uuid,uuid,integer,uuid,jsonb,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v31_delivery_lineage_scope(uuid,uuid,integer,uuid,jsonb,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v31_delivery_lineage_scope(uuid,uuid,integer,uuid,jsonb,text)','EXECUTE')`).Scan(&schedulerLineage, &apiLineage, &workerLineage)
	if err != nil || !schedulerLineage || apiLineage || workerLineage {
		t.Fatal("delegated lineage ACL drift", err)
	}
	var reservedOutcome string
	err = db.QueryRow(ctx, `SELECT outcome FROM photo.reserve_v31_delivery_lineage($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		uuid.New(), uuid.New(), 0, uuid.New(), `{}`, uuid.New(), strings.Repeat("a", 64), int64(2), `photos`).Scan(&reservedOutcome)
	if err != nil || reservedOutcome != "forbidden" {
		t.Fatal("fabricated delegated lineage reserve accepted", err, reservedOutcome)
	}
	var schedulerReserve, apiReserve, workerReserve bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.reserve_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.reserve_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.reserve_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text)','EXECUTE')`).Scan(&schedulerReserve, &apiReserve, &workerReserve)
	if err != nil || !schedulerReserve || apiReserve || workerReserve {
		t.Fatal("delegated lineage reserve ACL drift", err)
	}
	var completedLineage bool
	err = db.QueryRow(ctx, `SELECT photo.complete_v31_delivery_lineage($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		uuid.New(), uuid.New(), 0, uuid.New(), `{}`, uuid.New(), strings.Repeat("a", 64), int64(2), `photos`, uuid.New(), `version`).Scan(&completedLineage)
	if err != nil || completedLineage {
		t.Fatal("fabricated delegated lineage completion accepted", err)
	}
	var schedulerComplete, apiComplete, workerComplete bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.complete_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text,uuid,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.complete_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text,uuid,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.complete_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text,uuid,text)','EXECUTE')`).Scan(&schedulerComplete, &apiComplete, &workerComplete)
	if err != nil || !schedulerComplete || apiComplete || workerComplete {
		t.Fatal("delegated lineage completion ACL drift", err)
	}
	var delegatedExport *string
	err = db.QueryRow(ctx, `SELECT photo.v31_delivery_export_evidence($1,$2,$3,$4)`,
		uuid.New(), uuid.New(), 0, uuid.New()).Scan(&delegatedExport)
	if err != nil || delegatedExport != nil {
		t.Fatal("fabricated delegated export evidence accepted", err)
	}
	var schedulerEvidence, apiEvidence, workerEvidence bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v31_delivery_export_evidence(uuid,uuid,integer,uuid)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v31_delivery_export_evidence(uuid,uuid,integer,uuid)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v31_delivery_export_evidence(uuid,uuid,integer,uuid)','EXECUTE')`).Scan(&schedulerEvidence, &apiEvidence, &workerEvidence)
	if err != nil || !schedulerEvidence || apiEvidence || workerEvidence {
		t.Fatal("delegated export evidence ACL drift", err)
	}
	var finalTask *string
	err = db.QueryRow(ctx, `SELECT photo.v31_delivery_final_task_evidence($1,$2,$3,$4)`,
		uuid.New(), uuid.New(), 0, uuid.New()).Scan(&finalTask)
	if err != nil || finalTask != nil {
		t.Fatal("fabricated delegated final task accepted", err)
	}
	var schedulerFinal, apiFinal, workerFinal bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v31_delivery_final_task_evidence(uuid,uuid,integer,uuid)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v31_delivery_final_task_evidence(uuid,uuid,integer,uuid)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v31_delivery_final_task_evidence(uuid,uuid,integer,uuid)','EXECUTE')`).Scan(&schedulerFinal, &apiFinal, &workerFinal)
	if err != nil || !schedulerFinal || apiFinal || workerFinal {
		t.Fatal("delegated final task ACL drift", err)
	}
	var sameRunTask *string
	err = db.QueryRow(ctx, `SELECT photo.v31_delivery_same_run_task_evidence($1,$2,$3,$4,$5)`,
		uuid.New(), uuid.New(), 0, uuid.New(), uuid.New()).Scan(&sameRunTask)
	if err != nil || sameRunTask != nil {
		t.Fatal("fabricated same-run task accepted", err)
	}
	var schedulerSameRun, apiSameRun, workerSameRun bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v31_delivery_same_run_task_evidence(uuid,uuid,integer,uuid,uuid)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v31_delivery_same_run_task_evidence(uuid,uuid,integer,uuid,uuid)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v31_delivery_same_run_task_evidence(uuid,uuid,integer,uuid,uuid)','EXECUTE')`).Scan(&schedulerSameRun, &apiSameRun, &workerSameRun)
	if err != nil || !schedulerSameRun || apiSameRun || workerSameRun {
		t.Fatal("delegated same-run task ACL drift", err)
	}
	var ancestorTask *string
	err = db.QueryRow(ctx, `SELECT photo.v31_delivery_ancestor_task_evidence($1,$2,$3,$4,$5)`,
		uuid.New(), uuid.New(), 0, uuid.New(), uuid.New()).Scan(&ancestorTask)
	if err != nil || ancestorTask != nil {
		t.Fatal("fabricated delegated ancestor accepted", err)
	}
	var schedulerAncestor, apiAncestor, workerAncestor bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v31_delivery_ancestor_task_evidence(uuid,uuid,integer,uuid,uuid)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v31_delivery_ancestor_task_evidence(uuid,uuid,integer,uuid,uuid)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v31_delivery_ancestor_task_evidence(uuid,uuid,integer,uuid,uuid)','EXECUTE')`).Scan(&schedulerAncestor, &apiAncestor, &workerAncestor)
	if err != nil || !schedulerAncestor || apiAncestor || workerAncestor {
		t.Fatal("delegated ancestor ACL drift", err)
	}
	var delegatedDocument *string
	err = db.QueryRow(ctx, `SELECT photo.v31_delivery_task_document($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		uuid.New(), uuid.New(), 0, uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64), 1024, "photos").Scan(&delegatedDocument)
	if err != nil || delegatedDocument != nil {
		t.Fatal("fabricated delegated document accepted", err)
	}
	var schedulerDocument, apiDocument, workerDocument bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v31_delivery_task_document(uuid,uuid,integer,uuid,uuid,uuid,text,integer,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v31_delivery_task_document(uuid,uuid,integer,uuid,uuid,uuid,text,integer,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v31_delivery_task_document(uuid,uuid,integer,uuid,uuid,uuid,text,integer,text)','EXECUTE')`).Scan(&schedulerDocument, &apiDocument, &workerDocument)
	if err != nil || !schedulerDocument || apiDocument || workerDocument {
		t.Fatal("delegated document ACL drift", err)
	}
	var directProducer *string
	err = db.QueryRow(ctx, `SELECT photo.v31_delivery_direct_producer($1,$2,$3,$4,$5,$6)`,
		uuid.New(), uuid.New(), 0, uuid.New(), uuid.New(), strings.Repeat("a", 64)).Scan(&directProducer)
	if err != nil || directProducer != nil {
		t.Fatal("fabricated delegated producer accepted", err)
	}
	var schedulerProducer, apiProducer, workerProducer bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v31_delivery_direct_producer(uuid,uuid,integer,uuid,uuid,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v31_delivery_direct_producer(uuid,uuid,integer,uuid,uuid,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v31_delivery_direct_producer(uuid,uuid,integer,uuid,uuid,text)','EXECUTE')`).Scan(&schedulerProducer, &apiProducer, &workerProducer)
	if err != nil || !schedulerProducer || apiProducer || workerProducer {
		t.Fatal("delegated producer ACL drift", err)
	}
	var adoptedProducer *string
	err = db.QueryRow(ctx, `SELECT photo.v31_delivery_adopted_image_producer($1,$2,$3,$4,$5,$6)`,
		uuid.New(), uuid.New(), 0, uuid.New(), uuid.New(), strings.Repeat("a", 64)).Scan(&adoptedProducer)
	if err != nil || adoptedProducer != nil {
		t.Fatal("fabricated adopted producer accepted", err)
	}
	var schedulerAdopted, apiAdopted, workerAdopted bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v31_delivery_adopted_image_producer(uuid,uuid,integer,uuid,uuid,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v31_delivery_adopted_image_producer(uuid,uuid,integer,uuid,uuid,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v31_delivery_adopted_image_producer(uuid,uuid,integer,uuid,uuid,text)','EXECUTE')`).Scan(&schedulerAdopted, &apiAdopted, &workerAdopted)
	if err != nil || !schedulerAdopted || apiAdopted || workerAdopted {
		t.Fatal("delegated adopted producer ACL drift", err)
	}
	var originalSource *string
	err = db.QueryRow(ctx, `SELECT photo.v31_delivery_original_source($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		uuid.New(), uuid.New(), 0, uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64),
		uuid.New(), strings.Repeat("b", 64), "photos").Scan(&originalSource)
	if err != nil || originalSource != nil {
		t.Fatal("fabricated delegated original accepted", err)
	}
	var schedulerOriginal, apiOriginal, workerOriginal bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v31_delivery_original_source(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v31_delivery_original_source(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v31_delivery_original_source(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,text)','EXECUTE')`).Scan(&schedulerOriginal, &apiOriginal, &workerOriginal)
	if err != nil || !schedulerOriginal || apiOriginal || workerOriginal {
		t.Fatal("delegated original ACL drift", err)
	}
	var candidateReview *string
	err = db.QueryRow(ctx, `SELECT photo.v31_delivery_candidate_review($1,$2,$3,$4,$5,$6,$7)`,
		uuid.New(), uuid.New(), 0, uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64)).Scan(&candidateReview)
	if err != nil || candidateReview != nil {
		t.Fatal("fabricated delegated review accepted", err)
	}
	var schedulerReview, apiReview, workerReview bool
	err = db.QueryRow(ctx, `SELECT
	 has_function_privilege('photo_scheduler','photo.v31_delivery_candidate_review(uuid,uuid,integer,uuid,uuid,uuid,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v31_delivery_candidate_review(uuid,uuid,integer,uuid,uuid,uuid,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v31_delivery_candidate_review(uuid,uuid,integer,uuid,uuid,uuid,text)','EXECUTE')`).Scan(&schedulerReview, &apiReview, &workerReview)
	if err != nil || !schedulerReview || apiReview || workerReview {
		t.Fatal("delegated review ACL drift", err)
	}
	var documentConstraint string
	err = db.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
	 WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_v30_delivery_document_exact'`).Scan(&documentConstraint)
	if err != nil || !strings.Contains(documentConstraint, "v30_delivery_signed_closure") || !strings.Contains(documentConstraint, "v30_delivery_signed_envelope") {
		t.Fatal("signed document byte constraint missing", err)
	}
	var gateAccepted bool
	err = tx.QueryRow(ctx, `SELECT photo.v30_delivery_signing_assembly_gate($1,$2)`, uuid.New(), uuid.New()).Scan(&gateAccepted)
	if err != nil || gateAccepted {
		t.Fatal("fabricated signed assembly gate accepted", err)
	}
}

func TestDeliveryStorageRejectsMissingPolicyArtifact(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("database configuration failed")
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO photo.delivery_packages(tenant_id,project_id,workflow_id,snapshot_id,name,policy_artifact_id,state) VALUES($1,$2,$3,$4,'test',$5,'building')`, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New())
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "23503" || pgerr.Message != "ARTIFACT_REFERENCE_MISSING" {
		t.Fatalf("missing policy must be refused by ready-artifact guard, got %v", err)
	}
}

func TestDeliveryPackageTransitionMatrix(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("database configuration failed")
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		from, to        string
		published, want bool
	}{
		{"building", "review", false, true}, {"building", "ready", false, true}, {"review", "ready", false, true},
		{"ready", "failed", false, true}, {"ready", "failed", true, false}, {"ready", "ready", true, true},
		{"failed", "building", false, false}, {"cancelled", "ready", false, false}, {"review", "building", false, false},
		{"unknown", "unknown", false, false},
	} {
		var allowed bool
		if err := tx.QueryRow(ctx, "SELECT photo.delivery_package_transition_allowed($1,$2,$3)", test.from, test.to, test.published).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed != test.want {
			t.Fatalf("transition %s -> %s published=%t got %t", test.from, test.to, test.published, allowed)
		}
	}
}

// Privilege separation is independent of delivery fixtures; run against a fully
// migrated isolated database. This test does not install migrations.
func TestDeliveryPrepareAdoptionPrivileges(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	functions := []string{
		"photo.adopt_v30_delivery_prepared(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text)",
		"photo.v30_delivery_prepare_adoption_params(uuid,uuid,integer,bigint,uuid)",
		"photo.claim_v30_delivery_expansion_bound(uuid,text)",
		"photo.v30_delivery_expansion_plan(uuid,uuid)",
		"photo.commit_v30_delivery_child(uuid,uuid,integer,text,uuid)",
		"photo.create_v30_delivery_child_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint)",
		"photo.activate_v30_delivery_child(uuid,uuid,integer,text,uuid)",
		"photo.v30_delivery_child_receipt(uuid,uuid,integer,text,uuid,jsonb)",
		"photo.reserve_v30_delivery_child_params(uuid,uuid,integer,text,text,uuid,uuid,uuid)",
		"photo.v30_delivery_child_sources(uuid,uuid,integer,text,integer)",
		"photo.v30_delivery_stage_result(uuid,uuid,integer,text)",
		"photo.record_v30_delivery_stage_verification(uuid,uuid,integer,text,jsonb,text,text)",
		"photo.begin_v30_delivery_continuation(uuid,uuid,integer,text,jsonb,text,text)",
		"photo.create_v30_delivery_continuation_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint)",
		"photo.v30_delivery_continuation_transport_ready(uuid,uuid,integer,integer,uuid,jsonb,text)",
		"photo.activate_v30_delivery_continuation(uuid,uuid,integer,integer,uuid,jsonb,text)",
		"photo.v30_delivery_continuation_activated(uuid,uuid,integer,integer,uuid,jsonb,text)",
		"photo.v30_delivery_pending_child_slots(uuid,uuid,integer)",
		"photo.v30_delivery_encode_context(uuid,uuid)",
		"photo.v30_delivery_pending_encodes(integer)",
		"photo.v30_delivery_export_observation(uuid,uuid,integer)",
		"photo.v30_delivery_observation_page(uuid,uuid,integer,integer)",
		"photo.reserve_v30_delivery_shard(uuid,uuid,text,text,text,uuid,uuid,uuid)",
		"photo.reserve_v30_delivery_assembly(uuid,uuid,text,jsonb,text,uuid,uuid,uuid)",
		"photo.v30_delivery_saved_assembly(uuid,uuid,uuid,text)",
		"photo.v30_delivery_saved_shard(uuid,uuid,uuid,text,integer)",
		"photo.v30_delivery_assembly_inventory(uuid,uuid,uuid,text)",
		"photo.retain_v30_delivery_assembly(uuid,uuid,uuid,text)",
		"photo.create_v30_delivery_assembly_task(uuid,uuid,jsonb,text,bigint)",
		"photo.v30_delivery_assembly_transport_ready(uuid,uuid,jsonb,text)",
		"photo.reserve_v30_delivery_assembly_params(uuid,uuid,text,text,uuid,uuid,uuid)",
		"photo.complete_v30_delivery_assembly_params(uuid,uuid,text,text,uuid,uuid,text,uuid,uuid,uuid)",
		"photo.complete_v30_delivery_assembly(uuid,uuid,text,jsonb,text,uuid,uuid,text,uuid,uuid,uuid)",
		"photo.complete_v30_delivery_shard(uuid,uuid,text,text,text,uuid,uuid,text,uuid,uuid,uuid)",
		"photo.defer_v30_delivery_encode(uuid,uuid)",
		"photo.adopt_v30_delivery_encode(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)",
		"photo.record_v30_delivery_encode_verification(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)",
		"photo.v30_delivery_encode_artifact(uuid,uuid,integer,bigint,uuid,uuid)",
		"photo.v30_delivery_encode_staged_outputs(uuid,uuid,integer,bigint,uuid)",
		"photo.v30_delivery_saved_continuation_params(uuid,uuid,integer,text,text)",
		"photo.reopen_v30_delivery_continuation(uuid,uuid,integer,jsonb,text,text)",
		"photo.complete_v30_delivery_child_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid)",
		"photo.update_v30_delivery_expansion_lease(uuid,uuid,boolean)",
	}
	for _, fn := range functions {
		for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_portal"} {
			var allowed bool
			if err := db.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, fn).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed != (role == "photo_scheduler") {
				t.Fatal("unexpected delivery adoption privilege", role, fn, allowed)
			}
		}
	}
	var allowed bool
	if err := db.QueryRow(ctx, "SELECT has_function_privilege('photo_scheduler','photo.adopt_v2_plan_pre_delivery(uuid,uuid,integer,bigint,uuid,jsonb,text)','EXECUTE')").Scan(&allowed); err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("scheduler can bypass dedicated delivery adoption")
	}
	if err := db.QueryRow(ctx, "SELECT has_function_privilege('photo_scheduler','photo.claim_v30_delivery_expansion()','EXECUTE')").Scan(&allowed); err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("scheduler can bypass instance-bound delivery claim")
	}
}

// Missing or foreign producer authority must not yield object locations. This
// test intentionally does not manufacture valid adoption or apply migrations.
func TestDeliveryStageContextRejectsUnknownAuthority(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"derivative.resize", "derivative.sharpen", "export.encode", "unknown"} {
		var result []byte
		err = tx.QueryRow(ctx, `SELECT photo.v30_delivery_stage_result($1,$2,$3,$4)`, uuid.New(), uuid.New(), 0, operation).Scan(&result)
		if err != nil {
			t.Fatal(err)
		}
		if len(result) != 0 && string(result) != "null" {
			t.Fatal("missing authority exposed producer", operation)
		}
	}
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM photo.v30_delivery_encode_context($1,$2)`, uuid.New(), uuid.New()).Scan(&count)
	if err != nil || count != 0 {
		t.Fatal("missing encode authority accepted", count, err)
	}
}

// Neither a fabricated descriptor nor caller-selected refs may acknowledge a
// sealed transport without the current parent/root/inventory SQL authority.
func TestAssemblyTransportRejectsFabricatedAuthority(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	for _, params := range []string{`{"schema_version":1,"assembly":{"artifact_id":"00000000-0000-0000-0000-000000000001","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`, `{"schema_version":1}`, `{"schema_version":1,"assembly":null}`, `{"schema_version":1,"assembly":{"artifact_id":"foreign","sha256":"bad"}}`} {
		var ready bool
		if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_assembly_transport_ready($1,$2,'{}'::jsonb,$3)`, uuid.New(), uuid.New(), params).Scan(&ready); err != nil {
			t.Fatal(err)
		}
		if ready {
			t.Fatal("fabricated assembly transport accepted", params)
		}
	}
	var inventory []byte
	if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_assembly_inventory($1,$2,$3,$4)`, uuid.New(), uuid.New(), uuid.New(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Scan(&inventory); err != nil {
		t.Fatal(err)
	}
	if len(inventory) != 0 && string(inventory) != "null" {
		t.Fatal("foreign assembly inventory exposed")
	}
}

func TestAssemblySourceContextRemainsPrivate(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, fn := range []string{"photo.v30_delivery_publication_authority(uuid,uuid,uuid,uuid,bigint)", "photo.v30_delivery_manifest_authority(uuid,uuid,integer,bigint,uuid,text,text)", "photo.v30_delivery_failure_witness(uuid,uuid,integer)", "photo.v30_delivery_assembly_source_context(uuid,uuid,uuid,uuid,text,text)", "photo.v30_delivery_current_export_observation(uuid,uuid,uuid,jsonb)", "photo.v30_delivery_assembly_source_ready(uuid,uuid,uuid,uuid,text,text)"} {
		for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler", "photo_gateway", "photo_portal"} {
			var allowed bool
			if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, fn).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed {
				t.Fatal("partial source predicate exposed", fn, role)
			}
		}
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
		t.Fatal(err)
	}
	raw := `{"schema_version":1,"assembly":{"artifact_id":"00000000-0000-0000-0000-000000000001","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`
	var result []byte
	if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_assembly_source_context($1,$2,$3,$4,encode(sha256(convert_to($5,'UTF8')),'hex'),$5)`, uuid.New(), uuid.New(), uuid.New(), uuid.New(), raw).Scan(&result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 && string(result) != "null" {
		t.Fatal("foreign source context exposed")
	}
	for _, slot := range []int{-1, 0, 79999, 80000} {
		var witness []byte
		if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_failure_witness($1,$2,$3)`, uuid.New(), uuid.New(), slot).Scan(&witness); err != nil {
			t.Fatal(err)
		}
		if len(witness) != 0 && string(witness) != "null" {
			t.Fatal("foreign failure witness exposed")
		}
	}
	for _, revision := range []int64{0, 1, 9007199254740992} {
		var authority []byte
		if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_publication_authority($1,$2,$3,$4,$5)`, uuid.New(), uuid.New(), uuid.New(), uuid.New(), revision).Scan(&authority); err != nil {
			t.Fatal(err)
		}
		if len(authority) != 0 && string(authority) != "null" {
			t.Fatal("foreign publication authority exposed")
		}
	}
	for _, body := range []string{`{}`, `null`, ""} {
		var authority []byte
		if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_manifest_authority($1,$2,1,1,$3,$4,$5)`, uuid.New(), uuid.New(), uuid.New(), raw, body).Scan(&authority); err != nil {
			t.Fatal(err)
		}
		if len(authority) != 0 && string(authority) != "null" {
			t.Fatal("foreign manifest authority exposed")
		}
	}
	var ready bool
	if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_assembly_source_ready($1,$2,$3,$4,encode(sha256(convert_to($5,'UTF8')),'hex'),$5)`, uuid.New(), uuid.New(), uuid.New(), uuid.New(), raw).Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("foreign runtime assembly source accepted")
	}
	for _, request := range []string{`null`, `{}`, `{"schema_version":1,"slot_index":80000}`, `{"schema_version":1,"slot_index":-1}`} {
		var observation []byte
		if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_current_export_observation($1,$2,$3,$4::jsonb)`, uuid.New(), uuid.New(), uuid.New(), request).Scan(&observation); err != nil {
			t.Fatal(err)
		}
		if len(observation) != 0 && string(observation) != "null" {
			t.Fatal("invalid current child observation exposed")
		}
	}

}

// The public wrapper must refuse fabricated leases and restore caller context.
// The matrix helper cannot be invoked directly by a runtime role.
func TestAssemblyWorkerSourceGateRefusesUnknownLease(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	previous := uuid.NewString()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, previous); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE photo_worker`); err != nil {
		t.Fatal(err)
	}
	raw := `{"schema_version":1,"assembly":{"artifact_id":"00000000-0000-0000-0000-000000000001","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`
	var accepted bool
	if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_source_ready($1,$2,$3,1,1,$4,$5,encode(sha256(convert_to($6,'UTF8')),'hex'),$6,$7)`, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), raw, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Scan(&accepted); err != nil {
		t.Fatal(err)
	}
	if accepted {
		t.Fatal("unknown worker lease accepted")
	}
	var restored string
	if err := tx.QueryRow(ctx, `SELECT current_setting('app.tenant_id',true)`).Scan(&restored); err != nil {
		t.Fatal(err)
	}
	if restored != previous {
		t.Fatal("worker source gate leaked tenant context")
	}
	var allowed bool
	if err := tx.QueryRow(ctx, `SELECT has_function_privilege(current_user,'photo.v30_delivery_source_ready_before_tenant_context(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)','EXECUTE')`).Scan(&allowed); err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("worker can bypass tenant-context wrapper")
	}
}

func TestDeliveryCancellationSyncRefusesUnknownScope(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, fn := range []string{"photo.sync_v30_delivery_cancellation(uuid,uuid)", "photo.cleanup_next_v30_delivery()", "photo.release_v30_delivery_quota(uuid,uuid)", "photo.v30_delivery_assembly_adoption_committed(uuid,uuid)", "photo.v30_delivery_assembly_adoption_inputs(uuid,uuid,integer,bigint,uuid,text)", "photo.v30_delivery_assembly_adoption_params(uuid,uuid,integer,bigint,uuid)", "photo.v30_delivery_failure_committed(uuid,uuid,integer)", "photo.fail_v30_delivery(uuid,uuid,integer)", "photo.adopt_v30_delivery_assembled(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text)"} {
		for _, role := range []string{"photo_api", "photo_worker", "photo_gateway", "photo_portal"} {
			var allowed bool
			if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, fn).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed {
				t.Fatal("delivery cleanup exposed", role, fn)
			}
		}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE photo_scheduler`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`SELECT photo.release_v30_delivery_quota($1,$2)`, `SELECT photo.v30_delivery_assembly_adoption_committed($1,$2)`} {
		var accepted bool
		if err := tx.QueryRow(ctx, query, uuid.New(), uuid.New()).Scan(&accepted); err != nil {
			t.Fatal(err)
		}
		if accepted {
			t.Fatal("foreign delivery quota/receipt accepted")
		}
	}
	var adopted bool
	if err := tx.QueryRow(ctx, `SELECT photo.adopt_v30_delivery_assembled($1,$2,1,1,$3,'[]'::jsonb,$4,'{}','{}')`, uuid.New(), uuid.New(), uuid.New(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Scan(&adopted); err != nil {
		t.Fatal(err)
	}
	if adopted {
		t.Fatal("foreign delivery manifest adopted")
	}
	var changed bool
	if err := tx.QueryRow(ctx, `SELECT photo.sync_v30_delivery_cancellation($1,$2)`, uuid.New(), uuid.New()).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("unknown cancellation scope changed")
	}
	rows, err := tx.Query(ctx, `SELECT * FROM photo.v30_delivery_assembly_adoption_params($1,$2,1,1,$3)`, uuid.New(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		rows.Close()
		t.Fatal("foreign assembly Params exposed")
	}
	rows.Close()
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	for _, attempt := range []int{0, 1} {
		var evidence []byte
		if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_assembly_adoption_inputs($1,$2,$3,1,$4,$5)`, uuid.New(), uuid.New(), attempt, uuid.New(), `{}`).Scan(&evidence); err != nil {
			t.Fatal(err)
		}
		if len(evidence) != 0 && string(evidence) != "null" {
			t.Fatal("foreign adoption inputs exposed")
		}
	}

	for _, slot := range []int{-1, 0, 79999, 80000} {
		var committed bool
		if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_failure_committed($1,$2,$3)`, uuid.New(), uuid.New(), slot).Scan(&committed); err != nil {
			t.Fatal(err)
		}
		if committed {
			t.Fatal("unknown failure acknowledged")
		}
		if err := tx.QueryRow(ctx, `SELECT photo.fail_v30_delivery($1,$2,$3)`, uuid.New(), uuid.New(), slot).Scan(&committed); err != nil {
			t.Fatal(err)
		}
		if committed {
			t.Fatal("unknown failure finalized")
		}
	}

}

func TestDeliveryPublishRefusesUnknownScopeAndRevision(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE photo_api`); err != nil {
		t.Fatal(err)
	}
	for _, revision := range []int64{0, 1, 9007199254740991} {
		var publication *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT photo.publish_v30_delivery($1,$2,$3,$4,$5,$6)`, uuid.New(), uuid.New(), uuid.New(), uuid.New(), revision, uuid.New()).Scan(&publication); err != nil {
			t.Fatal(err)
		}
		if publication != nil {
			t.Fatal("foreign publication accepted")
		}
	}
}

// Download intent creation follows the declared editor role even though the
// package's metadata can be read by viewers. Uses only an isolated migrated DB.
func TestDeliveryDownloadIntentDeniesViewerBeforePersistence(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("database configuration failed")
	}
	defer admin.Close()
	var owner, tenantID, viewer, sessionID uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@test.example").Scan(&owner, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", owner).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.users(email_normalized) VALUES($1) RETURNING id", uuid.NewString()+"@test.example").Scan(&viewer); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES($1,$2,'viewer')", tenantID, viewer); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,'native',clock_timestamp()+interval '1 hour') RETURNING id", viewer, auth.HashSecret(uuid.NewString()), uuid.New()).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("database configuration failed")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("database configuration failed")
	}
	defer api.Close()
	service := DownloadIntentService{DB: api, Pepper: []byte(strings.Repeat("p", 32)), Bucket: "photo"}
	key := uuid.NewString()
	result, err := service.Create(ctx, auth.Identity{UserID: viewer, SessionID: sessionID}, tenantID, uuid.New(), key, DownloadIntentRequest{Kind: "manifest"})
	if err == nil || domain.AsError(err).Code != "FORBIDDEN" || result.Token != "" {
		t.Fatal("viewer download intent was not refused before admission", err)
	}
	var persisted int
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Delivery.Download.Intent' AND key=$3", tenantID, viewer, key).Scan(&persisted); err != nil || persisted != 0 {
		t.Fatal("unauthorized download attempt persisted", err)
	}
}
