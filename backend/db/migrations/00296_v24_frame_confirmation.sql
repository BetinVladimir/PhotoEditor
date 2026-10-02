-- +goose Up
CREATE TABLE photo.capture_frame_confirmations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL,
 session_id uuid NOT NULL,
 receipt_id uuid NOT NULL,
 asset_version_id uuid NOT NULL,
 alert_id uuid NOT NULL,
 confirmed_by uuid NOT NULL REFERENCES photo.users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 identity_evidence jsonb NOT NULL CHECK(jsonb_typeof(identity_evidence)='array') CHECK(octet_length(identity_evidence::text)<=1048576),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,receipt_id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,project_id,session_id) REFERENCES photo.shoot_sessions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,project_id,receipt_id) REFERENCES photo.capture_receipts(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,alert_id) REFERENCES photo.capture_alerts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX ON photo.capture_frame_confirmations(tenant_id,project_id,session_id,created_at,id);
ALTER TABLE photo.capture_frame_confirmations ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.capture_frame_confirmations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.capture_frame_confirmations TO photo_api USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.capture_frame_confirmations TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.capture_frame_confirmations FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT ON photo.capture_frame_confirmations TO photo_api;
GRANT SELECT,INSERT,DELETE ON photo.capture_frame_confirmations TO photo_fn_owner;
CREATE TRIGGER capture_frame_confirmation_immutable BEFORE UPDATE ON photo.capture_frame_confirmations FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
-- +goose StatementBegin
CREATE FUNCTION photo.confirm_capture_frame(p_tenant uuid,p_receipt uuid,p_alert uuid,p_actor uuid,p_auth_session uuid)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE receipt photo.capture_receipts%ROWTYPE; alert photo.capture_alerts%ROWTYPE;
 confirmation photo.capture_frame_confirmations%ROWTYPE; version uuid; person uuid; people uuid[]:='{}'; evidence jsonb;
BEGIN
 IF p_tenant IS NULL OR p_receipt IS NULL OR p_alert IS NULL OR p_actor IS NULL OR p_auth_session IS NULL
 OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN
  RAISE EXCEPTION 'invalid frame confirmation' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'frame confirmation forbidden' USING ERRCODE='42501'; END IF;
 SELECT * INTO receipt FROM photo.capture_receipts WHERE tenant_id=p_tenant AND id=p_receipt;
 IF NOT FOUND THEN RAISE EXCEPTION 'capture receipt missing' USING ERRCODE='P0002'; END IF;
 PERFORM id FROM photo.shoot_sessions WHERE tenant_id=p_tenant AND project_id=receipt.project_id AND id=receipt.session_id AND state<>'archived' FOR UPDATE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,receipt.project_id,p_actor,p_auth_session) THEN
  RAISE EXCEPTION 'frame confirmation forbidden' USING ERRCODE='42501';
 END IF;
 SELECT asset_version_id INTO version FROM photo.capture_receipt_assets WHERE tenant_id=p_tenant AND receipt_id=p_receipt;
 SELECT * INTO alert FROM photo.capture_alerts WHERE tenant_id=p_tenant AND project_id=receipt.project_id
 AND session_id=receipt.session_id AND asset_version_id=version AND id=p_alert FOR SHARE;
 IF NOT FOUND OR alert.quality_metric IS DISTINCT FROM 'capture.technical_quality/1'
 OR NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=alert.report_artifact_id
 AND a.sha256=alert.quality_report_sha AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'capture quality evidence unavailable' USING ERRCODE='23514';
 END IF;
 SELECT * INTO confirmation FROM photo.capture_frame_confirmations WHERE tenant_id=p_tenant AND receipt_id=p_receipt;
 IF FOUND THEN
  IF confirmation.alert_id<>p_alert THEN RAISE EXCEPTION 'frame confirmation conflict' USING ERRCODE='40001'; END IF;
  RETURN confirmation.id;
 END IF;
 FOR person IN SELECT DISTINCT person_id FROM photo.person_observations WHERE tenant_id=p_tenant
 AND project_id=receipt.project_id AND asset_version_id=version AND confirmed_by IS NOT NULL ORDER BY person_id LOOP
  PERFORM id FROM photo.project_persons WHERE tenant_id=p_tenant AND project_id=receipt.project_id AND id=person AND state='active' FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'V24_IDENTITY_CONSENT_REVOKED' USING ERRCODE='23514'; END IF;
  PERFORM id FROM photo.consent_grants WHERE tenant_id=p_tenant AND project_id=receipt.project_id AND person_id=person AND purpose='identity_matching' AND status='granted' FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'V24_IDENTITY_CONSENT_REVOKED' USING ERRCODE='23514'; END IF;
  people:=array_append(people,person);
 END LOOP;
 SELECT coalesce(jsonb_agg(jsonb_build_object('person_id',g.person_id,'consent_id',g.id,'epoch',g.epoch::text) ORDER BY g.person_id),'[]'::jsonb)
 INTO evidence FROM photo.consent_grants g WHERE g.tenant_id=p_tenant AND g.project_id=receipt.project_id
 AND g.purpose='identity_matching' AND g.status='granted' AND g.person_id=ANY(people);
 IF jsonb_array_length(evidence)>100 THEN RAISE EXCEPTION 'too many confirmed frame people' USING ERRCODE='22023'; END IF;
 INSERT INTO photo.capture_frame_confirmations(tenant_id,project_id,session_id,receipt_id,asset_version_id,alert_id,confirmed_by,identity_evidence)
 VALUES(p_tenant,receipt.project_id,receipt.session_id,p_receipt,version,p_alert,p_actor,evidence) RETURNING id INTO person;
 RETURN person;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.confirm_capture_frame(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.confirm_capture_frame(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.confirm_capture_frame(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finish_tenant_deletion(p_job uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE;t text;
 tables_to_purge constant text[]:=ARRAY[
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
ALTER FUNCTION photo.finish_tenant_deletion(uuid,uuid) OWNER TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
