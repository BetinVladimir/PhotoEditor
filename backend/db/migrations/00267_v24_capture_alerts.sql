-- +goose Up
CREATE TABLE photo.capture_alerts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 session_id uuid NOT NULL,
 asset_version_id uuid NOT NULL,
 report_artifact_id uuid NOT NULL,
 severity text NOT NULL CHECK(severity IN('info','warning','critical')),
 acknowledged_by uuid REFERENCES photo.users(id),
 acknowledged_at timestamptz,
 CHECK((acknowledged_by IS NULL)=(acknowledged_at IS NULL)),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,project_id,id),
 UNIQUE(tenant_id,session_id,asset_version_id,report_artifact_id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,project_id,session_id) REFERENCES photo.shoot_sessions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,report_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX capture_alerts_session_created ON photo.capture_alerts(tenant_id,project_id,session_id,created_at,id);
ALTER TABLE photo.capture_alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.capture_alerts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.capture_alerts TO photo_api
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.capture_alerts TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.capture_alerts FROM PUBLIC;
GRANT SELECT ON photo.capture_alerts TO photo_api;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.capture_alerts TO photo_fn_owner;
CREATE TRIGGER capture_alerts_revision BEFORE UPDATE ON photo.capture_alerts
 FOR EACH ROW EXECUTE FUNCTION photo.bump_v2_revision();

-- +goose StatementBegin
CREATE FUNCTION photo.acknowledge_capture_alert(p_tenant uuid,p_id uuid,p_expected bigint,
 p_actor uuid,p_session uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE alert photo.capture_alerts%ROWTYPE;
BEGIN
 IF p_expected IS NULL OR p_expected<1 THEN
  RAISE EXCEPTION 'CAPTURE_ALERT_INVALID' USING ERRCODE='22023'; END IF;
 SELECT * INTO alert FROM photo.capture_alerts WHERE tenant_id=p_tenant AND id=p_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'CAPTURE_ALERT_MISSING' USING ERRCODE='P0002'; END IF;
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,alert.project_id,p_actor,p_session) THEN
  RAISE EXCEPTION 'CAPTURE_ALERT_FORBIDDEN' USING ERRCODE='42501'; END IF;
 SELECT * INTO alert FROM photo.capture_alerts WHERE tenant_id=p_tenant AND id=p_id FOR UPDATE;
 IF alert.revision<>p_expected OR alert.acknowledged_at IS NOT NULL THEN
  RAISE EXCEPTION 'CAPTURE_ALERT_REVISION_CONFLICT' USING ERRCODE='40001'; END IF;
 UPDATE photo.capture_alerts SET acknowledged_by=p_actor,acknowledged_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND id=p_id RETURNING * INTO alert;
 RETURN to_jsonb(alert)||jsonb_build_object('resource_type','capture_alerts','revision',alert.revision::text);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.acknowledge_capture_alert(uuid,uuid,bigint,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.acknowledge_capture_alert(uuid,uuid,bigint,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.acknowledge_capture_alert(uuid,uuid,bigint,uuid,uuid) TO photo_api;

-- Extend the explicit metadata purge allowlist after confirmed object cleanup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finish_tenant_deletion(p_job uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE;t text;
 tables_to_purge constant text[]:=ARRAY[
  'capture_alerts','capture_receipt_assets','capture_receipts','capture_challenges','capture_devices','learning_safety_reviews','model_use_links','learning_example_links','profile_activations','version_eligibility','consent_cleanup','learning_versions','correction_events','learning_profiles','v22_analysis_decisions',
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
