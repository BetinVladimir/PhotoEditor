-- +goose Up
-- Recheck persisted removal-copy authority before each input grant and heartbeat.
-- Raw Params and byte identity remain independently checked by worker admission.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_background_cleanup_task_live(p_task uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;project uuid;expected numeric;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND THEN RETURN false;END IF;
 IF t.operation IS DISTINCT FROM 'semantic.adopt_shown_background_cleanup' THEN RETURN true;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'apply' OR r.status IS DISTINCT FROM 'running'
 OR NOT r.graph_sealed OR r.cancel_requested OR t.cancel_requested
 OR t.state IS DISTINCT FROM 'running' OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR t.execution_profile->>'contracts_sha' IS DISTINCT FROM '13f037ae3104206bfe3bdf118bf2e4e174c12682de519cbaaf68afdf8b11dad5'
 OR jsonb_typeof(r.config_snapshot->'shown_semantic'->'nodes') IS DISTINCT FROM 'array'
 THEN RETURN false;END IF;
 project:=(r.config_snapshot->>'project_id')::uuid;
 IF photo.v27_downstream_profile_ready(t.tenant_id,project,t.operation,t.operation_version,'cpu',t.execution_profile) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=t.tenant_id AND status='active')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_review_batches b JOIN photo.semantic_targets target
 ON target.tenant_id=b.tenant_id AND target.batch_id=b.id AND target.child_run_id=b.child_run_id
 JOIN photo.semantic_intents intent ON intent.tenant_id=target.tenant_id AND intent.id=target.intent_id
 JOIN photo.runs preview ON preview.tenant_id=intent.tenant_id
 AND preview.config_snapshot->'semantic_background_cleanup_preview'->'params'->>'intent_id'=intent.id::text
 AND preview.config_snapshot->'semantic_background_cleanup_preview'->'params'->>'target_id'=target.id::text
 JOIN photo.tasks producer ON producer.tenant_id=preview.tenant_id AND producer.run_id=preview.id
 AND producer.id::text=preview.v27_background_cleanup_preview_receipt->>'task_id'
 WHERE b.tenant_id=t.tenant_id AND b.child_run_id=r.id AND b.project_id=project
 AND b.actor_id=r.created_by AND b.action='apply' AND target.project_id=project AND target.state='queued'
 AND preview.kind='preview' AND preview.graph_sealed AND NOT preview.cancel_requested
 AND preview.created_by=r.created_by AND producer.state='succeeded' AND NOT producer.cancel_requested
 AND producer.canonical_result_id::text=preview.v27_background_cleanup_preview_receipt->>'result_id'
 AND producer.attempt::text=preview.v27_background_cleanup_preview_receipt->>'attempt'
 AND producer.fencing_token::text=preview.v27_background_cleanup_preview_receipt->>'fence'
 AND producer.operation='semantic.preview_background_cleanup' AND producer.operation_version='1'
 AND producer.id=photo.semantic_uuid_v5(preview.id,'semantic.preview_background_cleanup:'||target.id::text)
 AND NOT EXISTS(SELECT 1 FROM unnest(ARRAY['presentation','working_image','pixels','numerical_evidence']) field
 WHERE NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs input WHERE input.tenant_id=t.tenant_id AND input.task_id=t.id
 AND input.artifact_id::text=preview.v27_background_cleanup_preview_receipt->field->>'artifact_id'
 AND input.sha256=preview.v27_background_cleanup_preview_receipt->field->>'sha256'))
 AND photo.v2_editor_authorized(t.tenant_id,project,r.created_by,
 (preview.config_snapshot->'semantic_background_cleanup_preview'->>'session_id')::uuid) IS TRUE
 AND t.id=photo.semantic_uuid_v5(r.id,'semantic.adopt_shown_background_cleanup:'||target.id::text)
 AND (SELECT count(*) FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') n
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 WHERE n->>'task_id'=t.id::text AND n->>'target_id'=target.id::text
 AND n->>'params_artifact_id'=a.id::text AND n->>'params_sha'=a.sha256)=1)
 THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id) NOT BETWEEN 3 AND 514
 OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
 WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id AND (i.project_id IS DISTINCT FROM project
 OR a.id IS NULL OR a.sha256 IS DISTINCT FROM i.sha256 OR a.state IS DISTINCT FROM 'ready'
 OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN false;END IF;
 SELECT sum((execution_profile->'resource_estimate'->>'output_bytes')::numeric) INTO expected
 FROM photo.tasks WHERE tenant_id=t.tenant_id AND run_id=r.id;
 RETURN coalesce(expected BETWEEN 1 AND 9007199254740991 AND EXISTS(
 SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=t.tenant_id AND q.holder_type='run'
 AND q.holder_id=r.id AND q.state='reserved' AND q.principal_type='user' AND q.principal_id=r.created_by
 AND q.files=0 AND q.share_id IS NULL AND q.bytes=expected::bigint AND q.expires_at>clock_timestamp()),false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_background_cleanup_task_live(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_background_cleanup_task_live(uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_portal;
ALTER FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) RENAME TO gateway_input_before_shown_background_cleanup;
REVOKE ALL ON FUNCTION photo.gateway_input_before_shown_background_cleanup(uuid,integer,bigint,uuid,text,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_portal;
CREATE FUNCTION photo.gateway_input(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text,p_artifact uuid)
RETURNS TABLE(artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.semantic_shown_background_cleanup_task_live(p_task) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.gateway_input_before_shown_background_cleanup(p_task,p_attempt,p_fence,p_worker,p_certificate,p_artifact);
END $$;
ALTER FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal;
GRANT EXECUTE ON FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) TO photo_gateway;
ALTER FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb) RENAME TO heartbeat_task_before_shown_background_cleanup;
REVOKE ALL ON FUNCTION photo.heartbeat_task_before_shown_background_cleanup(uuid,integer,bigint,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_portal;
CREATE FUNCTION photo.heartbeat_task(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_progress jsonb)
RETURNS TABLE(accepted boolean,lease_until timestamptz,cancel_requested boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.semantic_shown_background_cleanup_task_live(p_task) IS DISTINCT FROM true THEN
 RETURN QUERY SELECT false,NULL::timestamptz,true;RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.heartbeat_task_before_shown_background_cleanup(p_task,p_attempt,p_fence,p_worker,p_progress);
END $$;
ALTER FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_gateway,photo_scheduler,photo_portal;
GRANT EXECUTE ON FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
