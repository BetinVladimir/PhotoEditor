-- +goose Up
-- Derived protection roots are backend documents, never worker output aliases.
CREATE TABLE photo.semantic_native_protections (
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL, document_id uuid NOT NULL,
 asset_version_id uuid NOT NULL, source_id uuid NOT NULL, source_sha text NOT NULL CHECK(source_sha ~ '^[0-9a-f]{64}$'),
 parent_run_id uuid NOT NULL, task_id uuid NOT NULL, result_id uuid NOT NULL,
 root_id uuid NOT NULL, root_sha text NOT NULL CHECK(root_sha ~ '^[0-9a-f]{64}$'),
 pixels_id uuid NOT NULL, pixels_sha text NOT NULL CHECK(pixels_sha ~ '^[0-9a-f]{64}$'),
 report_id uuid NOT NULL, report_sha text NOT NULL CHECK(report_sha ~ '^[0-9a-f]{64}$'),
 anchor jsonb NOT NULL CHECK(jsonb_typeof(anchor)='object' AND octet_length(anchor::text)<=8192),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,document_id), UNIQUE(tenant_id,root_id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,task_id) REFERENCES photo.tasks(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,result_id) REFERENCES photo.task_results(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,root_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE photo.semantic_native_protections ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.semantic_native_protections FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.semantic_native_protections TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT ON photo.semantic_native_protections TO photo_fn_owner;
-- Explicit tenant deletion allowlist permission; no application role gets DELETE.
GRANT DELETE ON photo.semantic_native_protections TO photo_fn_owner;
-- +goose StatementBegin
CREATE FUNCTION photo.register_semantic_native_protection(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_task uuid,p_result uuid,p_anchor jsonb,p_document uuid,p_root uuid,p_root_sha text,p_body text,p_report text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE catalog record; upload photo.v2_draft_uploads%ROWTYPE; saved photo.semantic_native_protections%ROWTYPE; mask jsonb; expected_parent uuid;
BEGIN
 IF octet_length(p_body) NOT BETWEEN 2 AND 1048576 OR octet_length(p_report) NOT BETWEEN 2 AND 1048576
 OR photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha) IS NOT TRUE
 OR photo.lock_semantic_proposal_person_inventory(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR photo.lock_semantic_defect_native_parent(p_tenant,p_project,p_actor,p_session,p_task,p_anchor) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO catalog FROM photo.semantic_defect_native_result_catalog(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_task);
 IF NOT FOUND OR catalog.result_id IS DISTINCT FROM p_result THEN RETURN false;END IF;
 expected_parent:=(p_anchor->>'parent_run_id')::uuid;
 IF p_document IS DISTINCT FROM photo.semantic_uuid_v5(expected_parent,'semantic.defect.protection:'||p_task::text)
 OR encode(public.digest(convert_to(p_report,'UTF8'),'sha256'),'hex') IS DISTINCT FROM catalog.outputs->4->>'sha256'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_root_sha THEN RETURN false;END IF;
 mask:=p_body::jsonb;
 IF mask IS DISTINCT FROM p_report::jsonb->'protection' OR mask->'protected' IS DISTINCT FROM 'true'::jsonb
 OR mask->'artifact' IS DISTINCT FROM catalog.outputs->1 THEN RETURN false;END IF;
 SELECT * INTO upload FROM photo.v2_draft_uploads WHERE tenant_id=p_tenant AND document_id=p_document FOR SHARE;
 IF NOT FOUND OR upload.project_id<>p_project OR upload.asset_version_id<>p_version OR upload.artifact_id<>p_root OR upload.sha256<>p_root_sha
 OR upload.state<>'ready' OR upload.upload_contract<>'Document' OR upload.bytes<>octet_length(p_body) THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_root AND sha256=p_root_sha AND state='ready'
 AND verified_at IS NOT NULL AND output_role='document' AND media_type='application/json' AND bytes=upload.bytes
 AND bucket=upload.bucket AND object_version=upload.object_version AND object_version NOT IN('','null')
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 INSERT INTO photo.semantic_native_protections(tenant_id,project_id,document_id,asset_version_id,source_id,source_sha,parent_run_id,task_id,result_id,root_id,root_sha,pixels_id,pixels_sha,report_id,report_sha,anchor)
 VALUES(p_tenant,p_project,p_document,p_version,p_source,p_source_sha,expected_parent,p_task,p_result,p_root,p_root_sha,(catalog.outputs->1->>'artifact_id')::uuid,catalog.outputs->1->>'sha256',(catalog.outputs->4->>'artifact_id')::uuid,catalog.outputs->4->>'sha256',p_anchor)
 ON CONFLICT(tenant_id,document_id) DO NOTHING;
 -- Records are insert-only; ON CONFLICT serializes replay. Scope locks block tenant purge.
 SELECT * INTO saved FROM photo.semantic_native_protections WHERE tenant_id=p_tenant AND document_id=p_document;
 RETURN saved.project_id=p_project AND saved.asset_version_id=p_version AND saved.source_id=p_source AND saved.source_sha=p_source_sha
 AND saved.parent_run_id=expected_parent AND saved.task_id=p_task AND saved.result_id=p_result AND saved.root_id=p_root AND saved.root_sha=p_root_sha
 AND saved.pixels_id=(catalog.outputs->1->>'artifact_id')::uuid AND saved.pixels_sha=catalog.outputs->1->>'sha256'
 AND saved.report_id=(catalog.outputs->4->>'artifact_id')::uuid AND saved.report_sha=catalog.outputs->4->>'sha256' AND saved.anchor=p_anchor;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.register_semantic_native_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_semantic_native_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_semantic_native_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- Preserve the existing confirmed-cleanup purge, adding only the explicit new table.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finish_tenant_deletion(p_job uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE;t text;
 tables_to_purge constant text[]:=ARRAY[
 'semantic_native_protections','semantic_targets','semantic_intents',
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
