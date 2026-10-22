-- +goose Up
-- Recheck preview cancellation after worker success, before locked adoption.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v27_shown_retouch_adoption_ready(p_tenant uuid,p_task uuid,p_attempt integer,
 p_fence bigint,p_result uuid,p_params uuid,p_sha text,p_body text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 e jsonb;old_preview photo.runs%ROWTYPE;producer photo.tasks%ROWTYPE;project uuid;actor uuid;session_id uuid;field text;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 OR p_sha IS NULL
 OR p_sha !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_sha
 THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v27_shown_retouch_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result) a
 WHERE a.artifact_id=p_params AND a.sha256=p_sha) THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND bytes=octet_length(p_body))
 THEN RETURN false;END IF;
 e:=p_body::jsonb;
 SELECT * INTO old_preview FROM photo.runs WHERE tenant_id=p_tenant AND id=(e->>'preview_run_id')::uuid FOR SHARE;
 IF NOT FOUND OR old_preview.kind IS DISTINCT FROM 'preview' OR NOT old_preview.graph_sealed OR old_preview.cancel_requested
 OR old_preview.status NOT IN('running','completed') OR old_preview.created_by IS DISTINCT FROM r.created_by
 OR photo.semantic_shown_retouch_params_bound(p_tenant,t.run_id,(e->>'target_id')::uuid,old_preview.id,
 e->'approved_edit',e,old_preview.config_snapshot->'semantic_retouch_preview'->'params',old_preview.v27_retouch_preview_receipt) IS DISTINCT FROM true
 OR photo.semantic_shown_retouch_closure_ready(p_task,e) IS DISTINCT FROM true THEN RETURN false;END IF;
 FOREACH field IN ARRAY ARRAY['model_lock_sha','runtime_lock_sha','device_profile_sha'] LOOP
 IF e->field IS DISTINCT FROM t.execution_profile->field THEN RETURN false;END IF;
 END LOOP;
 actor:=r.created_by;session_id:=(old_preview.config_snapshot->'semantic_retouch_preview'->>'session_id')::uuid;
 SELECT i.project_id INTO project FROM photo.semantic_targets target
 JOIN photo.semantic_intents i ON i.tenant_id=target.tenant_id AND i.id=target.intent_id
 JOIN photo.v2_review_batches b ON b.tenant_id=target.tenant_id AND b.id=target.batch_id
 WHERE target.tenant_id=p_tenant AND target.id=(e->>'target_id')::uuid AND target.state='queued'
 AND target.child_run_id=t.run_id AND b.child_run_id=t.run_id AND b.actor_id=actor AND b.action='apply'
 AND b.workflow_id=i.workflow_id AND b.project_id=i.project_id AND i.snapshot_id=r.input_snapshot_id
 AND i.id::text=old_preview.config_snapshot->'semantic_retouch_preview'->'params'->>'intent_id';
 IF NOT FOUND OR r.config_snapshot->>'project_id' IS DISTINCT FROM project::text
 OR photo.v2_editor_authorized(p_tenant,project,actor,session_id) IS DISTINCT FROM true
 OR photo.v27_downstream_profile_ready(p_tenant,project,'semantic.adopt_shown_retouch_region','1','cpu',t.execution_profile) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=old_preview.id AND id=(e->>'preview_task_id')::uuid FOR SHARE;
 IF NOT FOUND OR producer.state IS DISTINCT FROM 'succeeded' OR producer.cancel_requested
 OR producer.operation IS DISTINCT FROM 'semantic.preview_retouch_region' OR producer.operation_version IS DISTINCT FROM '1'
 OR producer.resource_class IS DISTINCT FROM 'cpu'
 OR photo.semantic_retouch_preview_bundle_ready(producer.id,producer.canonical_result_id,producer.attempt,producer.fencing_token) IS DISTINCT FROM true
 OR producer.canonical_result_id::text IS DISTINCT FROM e->>'preview_result_id'
 OR producer.attempt::text IS DISTINCT FROM e->>'preview_attempt' OR producer.fencing_token::text IS DISTINCT FROM e->>'preview_fence'
 OR NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.id=producer.canonical_result_id
 AND z.task_id=producer.id AND z.attempt=producer.attempt AND z.fencing_token=producer.fencing_token
 AND z.outputs=jsonb_build_array(e->'presentation')) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=t.run_id
 AND q.state='reserved' AND q.principal_type='user' AND q.principal_id=actor AND q.share_id IS NULL AND q.files=0
 AND q.bytes>= (t.execution_profile->'resource_estimate'->>'output_bytes')::bigint AND q.expires_at>clock_timestamp()) THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v27_shown_retouch_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result) a
 WHERE a.artifact_id=p_params AND a.sha256=p_sha);
EXCEPTION WHEN insufficient_privilege OR invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v27_shown_retouch_adoption_ready(uuid,uuid,integer,bigint,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_shown_retouch_adoption_ready(uuid,uuid,integer,bigint,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.v27_shown_retouch_adoption_ready(uuid,uuid,integer,bigint,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
