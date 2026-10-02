-- Durable receipt -> assessment admission. No network effects in triggers.
-- +goose Up
CREATE TABLE photo.capture_assessment_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 receipt_id uuid NOT NULL,
 session_id uuid NOT NULL,
 asset_version_id uuid NOT NULL,
 capture_policy jsonb NOT NULL CHECK(jsonb_typeof(capture_policy)='object') CHECK(octet_length(capture_policy::text)<=1048576),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','leased','queued','failed')),
 lease_token uuid,
 lease_until timestamptz,
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),
 workflow_id uuid,
 error_code text CHECK(error_code IS NULL OR length(error_code) BETWEEN 1 AND 100),
 CHECK((lease_token IS NULL)=(lease_until IS NULL)),
 CHECK((state='leased')=(lease_token IS NOT NULL)),
 CHECK((state='queued')=(workflow_id IS NOT NULL)),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,receipt_id),
 FOREIGN KEY(tenant_id,project_id,receipt_id) REFERENCES photo.capture_receipts(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,project_id,session_id) REFERENCES photo.shoot_sessions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,project_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX capture_assessment_pending ON photo.capture_assessment_jobs(created_at,id)
 WHERE state IN('pending','leased');
ALTER TABLE photo.capture_assessment_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.capture_assessment_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.capture_assessment_jobs TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.capture_assessment_jobs FROM PUBLIC;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.capture_assessment_jobs TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.enqueue_capture_assessment() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE receipt photo.capture_receipts%ROWTYPE; policy jsonb;
BEGIN
 SELECT * INTO receipt FROM photo.capture_receipts WHERE tenant_id=NEW.tenant_id AND id=NEW.receipt_id;
 SELECT capture_policy INTO policy FROM photo.shoot_sessions WHERE tenant_id=NEW.tenant_id
  AND project_id=NEW.project_id AND id=receipt.session_id;
 IF policy IS NULL THEN RAISE EXCEPTION 'CAPTURE_POLICY_MISSING' USING ERRCODE='23514'; END IF;
 INSERT INTO photo.capture_assessment_jobs(tenant_id,project_id,receipt_id,session_id,asset_version_id,capture_policy)
 VALUES(NEW.tenant_id,NEW.project_id,NEW.receipt_id,receipt.session_id,NEW.asset_version_id,policy);
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.enqueue_capture_assessment() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enqueue_capture_assessment() FROM PUBLIC;
CREATE TRIGGER enqueue_capture_assessment AFTER INSERT ON photo.capture_receipt_assets
 FOR EACH ROW EXECUTE FUNCTION photo.enqueue_capture_assessment();

-- Historical receipts have no frozen policy evidence. Preserve their existence
-- explicitly rather than attributing today's mutable policy to old captures.
INSERT INTO photo.capture_assessment_jobs(tenant_id,project_id,receipt_id,session_id,
 asset_version_id,capture_policy,state,error_code)
SELECT a.tenant_id,a.project_id,a.receipt_id,r.session_id,a.asset_version_id,
 '{}'::jsonb,'failed','CAPTURE_POLICY_HISTORY_UNAVAILABLE'
FROM photo.capture_receipt_assets a JOIN photo.capture_receipts r
 ON r.tenant_id=a.tenant_id AND r.id=a.receipt_id;

-- +goose StatementBegin
CREATE FUNCTION photo.claim_capture_assessment()
RETURNS SETOF photo.capture_assessment_jobs
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.capture_assessment_jobs%ROWTYPE; claimed photo.capture_assessment_jobs%ROWTYPE;
BEGIN
 SELECT j.* INTO hint FROM photo.capture_assessment_jobs j JOIN photo.tenants t ON t.id=j.tenant_id
 WHERE t.status='active' AND (j.state='pending' OR j.state='leased' AND j.lease_until<=clock_timestamp())
 ORDER BY j.created_at,j.id LIMIT 1;
 IF NOT FOUND THEN RETURN; END IF;
 PERFORM 1 FROM photo.tenants WHERE id=hint.tenant_id AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT * INTO claimed FROM photo.capture_assessment_jobs WHERE tenant_id=hint.tenant_id AND id=hint.id FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR NOT(claimed.state='pending' OR claimed.state='leased' AND claimed.lease_until<=clock_timestamp()) THEN RETURN; END IF;
 UPDATE photo.capture_assessment_jobs SET state='leased',lease_token=gen_random_uuid(),
  lease_until=clock_timestamp()+interval '60 seconds',attempts=attempts+1
 WHERE tenant_id=claimed.tenant_id AND id=claimed.id RETURNING * INTO claimed;
 RETURN NEXT claimed;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_capture_assessment() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_capture_assessment() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_capture_assessment() TO photo_scheduler;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finish_tenant_deletion(p_job uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE;t text;
 tables_to_purge constant text[]:=ARRAY[
  'capture_assessment_jobs','capture_alerts','capture_receipt_assets','capture_receipts','capture_challenges','capture_devices','learning_safety_reviews','model_use_links','learning_example_links','profile_activations','version_eligibility','consent_cleanup','learning_versions','correction_events','learning_profiles','v22_analysis_decisions',
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
