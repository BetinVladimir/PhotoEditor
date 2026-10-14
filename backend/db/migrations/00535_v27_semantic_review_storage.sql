-- +goose Up
CREATE TABLE photo.semantic_intents (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 workflow_id uuid NOT NULL,
 reference_version_id uuid NOT NULL,
 snapshot_id uuid NOT NULL,
 intent_artifact_id uuid NOT NULL,
 intent_sha text NOT NULL CHECK (intent_sha ~ '^[0-9a-f]{64}$'),
 UNIQUE (tenant_id,id),
 UNIQUE (tenant_id,project_id,id),
 FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 UNIQUE (tenant_id,workflow_id)
);
ALTER TABLE photo.semantic_intents ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.semantic_intents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.semantic_intents TO photo_api USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.semantic_intents TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.semantic_intents FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT ON photo.semantic_intents TO photo_api;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.semantic_intents TO photo_fn_owner;
CREATE INDEX ON photo.semantic_intents(tenant_id,project_id,created_at,id);
CREATE TABLE photo.semantic_targets (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 intent_id uuid NOT NULL,
 child_run_id uuid,
 batch_id uuid,
 asset_version_id uuid NOT NULL,
 candidate_artifact_id uuid,
 review_id uuid,
 override_id uuid,
 state text NOT NULL CHECK (length(state) BETWEEN 1 AND 256) CHECK (state IN ('proposed','queued','rejected','unsupported','applied','failed','cancelled')),
 UNIQUE (tenant_id,id),
 UNIQUE (tenant_id,project_id,id),
 FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 UNIQUE (tenant_id,intent_id,asset_version_id)
);
ALTER TABLE photo.semantic_targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.semantic_targets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.semantic_targets TO photo_api USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.semantic_targets TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.semantic_targets FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT ON photo.semantic_targets TO photo_api;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.semantic_targets TO photo_fn_owner;
CREATE INDEX ON photo.semantic_targets(tenant_id,project_id,created_at,id);
ALTER TABLE photo.semantic_intents ADD FOREIGN KEY(tenant_id,project_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.semantic_intents ADD FOREIGN KEY(tenant_id,reference_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.semantic_intents ADD FOREIGN KEY(tenant_id,snapshot_id) REFERENCES photo.snapshots(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.semantic_intents ADD FOREIGN KEY(tenant_id,intent_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.semantic_targets ADD FOREIGN KEY(tenant_id,project_id,intent_id) REFERENCES photo.semantic_intents(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.semantic_targets ADD FOREIGN KEY(tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.semantic_targets ADD FOREIGN KEY(tenant_id,child_run_id) REFERENCES photo.runs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.semantic_targets ADD FOREIGN KEY(tenant_id,batch_id) REFERENCES photo.v2_review_batches(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.semantic_targets ADD FOREIGN KEY(tenant_id,candidate_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX ON photo.semantic_targets(tenant_id,intent_id,state,id);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finish_tenant_deletion(p_job uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE;t text;
 tables_to_purge constant text[]:=ARRAY[
 'semantic_targets','semantic_intents',
  'capture_frame_confirmations','shot_lists','capture_assessment_jobs','capture_alerts','capture_receipt_assets','capture_receipts','capture_challenges','capture_devices','learning_safety_reviews','model_use_links','learning_example_links','profile_activations','version_eligibility','consent_cleanup','learning_versions','correction_events','learning_profiles','v22_analysis_decisions',
  'workflow_consent_links','consent_events','person_observations','scene_members','scene_groups','session_analyses','consent_grants','project_persons','shoot_sessions',
  'v2_document_approvals','v2_document_refs','v2_task_inputs','v2_review_batches','v2_artifact_links','model_qualifications','v2_documents','v2_document_streams','v2_workflows','v2_draft_uploads',
  'apply_v4_stages','apply_nodes','apply_builds','artifact_leases','asset_embeddings','asset_version_sources','bulk_item_results','collection_items',
  'dead_letters','defect_branches','derivatives','download_archive_plans','download_export_plans','download_intents','download_job_items','download_volumes','editor_drafts','export_batch_children','graph_expansions','ingest_catalog_items','ingest_decode_items','ingest_node_plans','ingest_originals','ingest_progress','ingest_rejections','interactive_tasks','media_retry_nodes','output_uploads','project_acl','publications','resource_reservations','resource_slots','result_cache','reviews','run_events','sections','share_sessions','snapshot_items','staging_cleanup','style_analysis_nodes','style_asset_bindings','style_candidates','style_import_builds','task_attempts','task_edges','task_results','training_pairs','upload_abort_cleanup','upload_parts','asset_versions','assets','backend_documents','bulk_operations','collections','download_build_state','download_jobs','export_batches','exports','ingest_upload_bindings','legacy_imports','media_retry_jobs','overrides','quota_reservations','share_links','snapshots','style_analysis_builds','style_versions','tasks','training_sets','upload_sessions','tenant_static_resources','artifacts','runs','styles','projects','export_presets','idempotency_records','inbox','invitations','outbox','scheduler_tenants','tenant_event_cursors','tenant_usage','worker_registrations'
 ];
BEGIN
 SELECT * INTO j FROM photo.deletion_jobs WHERE id=p_job AND resource_type='tenant' AND status='running' AND cleanup_token=p_token AND cleanup_lease_until>clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.tenants x WHERE x.id=j.tenant_id AND x.status='deleting') OR EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=j.tenant_id AND r.status IN('draft','queued','running','waiting_review','cancelling')) THEN RETURN false;END IF;
 SET CONSTRAINTS ALL DEFERRED;
 UPDATE photo.v2_document_streams SET current_document_id=NULL,approved_document_id=NULL WHERE tenant_id=j.tenant_id;
 FOREACH t IN ARRAY tables_to_purge LOOP EXECUTE format('DELETE FROM photo.%I WHERE tenant_id=$1',t) USING j.tenant_id;END LOOP;
 DELETE FROM photo.audit_log WHERE tenant_id=j.tenant_id AND action NOT LIKE 'tenant.%';UPDATE photo.memberships SET status='suspended',revision=revision+1 WHERE tenant_id=j.tenant_id AND status<>'suspended';
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES(j.tenant_id,'system',NULL,'tenant.deleted','tenant',j.tenant_id,gen_random_uuid(),jsonb_build_object('job_id',j.id,'cleanup_passes',j.cleanup_passes+1));
 UPDATE photo.tenants SET status='deleted',revision=revision+1 WHERE id=j.tenant_id AND status='deleting';UPDATE photo.deletion_jobs SET status='completed',cleanup_token=NULL,cleanup_lease_until=NULL,cleanup_passes=cleanup_passes+1,cursor=NULL,error=NULL WHERE id=j.id;SET CONSTRAINTS ALL IMMEDIATE;RETURN true;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
