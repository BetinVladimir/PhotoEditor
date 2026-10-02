-- +goose Up
CREATE TABLE photo.shot_lists (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 session_id uuid NOT NULL,
 document_id uuid NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,project_id,id), UNIQUE(tenant_id,session_id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,project_id,session_id) REFERENCES photo.shoot_sessions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,project_id,document_id) REFERENCES photo.v2_documents(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX ON photo.shot_lists(tenant_id,project_id,session_id);
CREATE INDEX ON photo.shot_lists(tenant_id,project_id,document_id);
CREATE INDEX ON photo.shot_lists(tenant_id,project_id,created_at,id);
ALTER TABLE photo.shot_lists ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.shot_lists FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.shot_lists TO photo_api USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.shot_lists TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.shot_lists FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT ON photo.shot_lists TO photo_api;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.shot_lists TO photo_fn_owner;

-- The row must point to a ShotList document in a dedicated shot-list stream.
-- Stream reuse across sessions is forbidden: person scope belongs to one session.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_shot_list_document() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
DECLARE stream uuid;
BEGIN
 SELECT d.stream_id INTO stream FROM photo.v2_documents d
 JOIN photo.v2_document_streams s ON s.tenant_id=d.tenant_id AND s.project_id=d.project_id AND s.id=d.stream_id
 WHERE d.tenant_id=NEW.tenant_id AND d.project_id=NEW.project_id AND d.id=NEW.document_id
 AND d.document_type='ShotList' AND s.stream_type='shot_list' AND s.asset_version_id IS NULL
 FOR UPDATE OF s;
 IF NOT FOUND THEN RAISE EXCEPTION 'invalid shot-list document' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' AND (NEW.tenant_id,NEW.project_id,NEW.session_id,NEW.id) IS DISTINCT FROM
 (OLD.tenant_id,OLD.project_id,OLD.session_id,OLD.id) THEN
  RAISE EXCEPTION 'immutable shot-list scope' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT FROM photo.shot_lists l JOIN photo.v2_documents d ON d.tenant_id=l.tenant_id AND d.id=l.document_id
 WHERE l.tenant_id=NEW.tenant_id AND l.id<>NEW.id AND d.stream_id=stream) THEN
  RAISE EXCEPTION 'shot-list stream already bound' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_shot_list_document() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_shot_list_document() FROM PUBLIC;
CREATE TRIGGER shot_list_document_guard BEFORE INSERT OR UPDATE ON photo.shot_lists
FOR EACH ROW EXECUTE FUNCTION photo.guard_shot_list_document();

-- Extend the explicitly authorized tenant purge allowlist; object cleanup and
-- existing cleanup fencing remain the prerequisite enforced by the caller.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finish_tenant_deletion(p_job uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE;t text;
 tables_to_purge constant text[]:=ARRAY[
  'shot_lists','capture_assessment_jobs','capture_alerts','capture_receipt_assets','capture_receipts','capture_challenges','capture_devices','learning_safety_reviews','model_use_links','learning_example_links','profile_activations','version_eligibility','consent_cleanup','learning_versions','correction_events','learning_profiles','v22_analysis_decisions',
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
ALTER FUNCTION photo.finish_tenant_deletion(uuid,uuid) OWNER TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
