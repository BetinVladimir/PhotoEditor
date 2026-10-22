-- +goose Up
-- Dedicated result-scoped evidence for shown Apply runs; generic V2 stays V2.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_adoption_evidence_scope(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_operation text)
RETURNS uuid LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;project uuid;matches integer;target uuid;preview_key text;receipt_key text;session_id uuid;
BEGIN
 IF p_operation IS NULL OR p_operation NOT IN('semantic.adopt_shown_exposure','semantic.adopt_shown_removal','semantic.adopt_shown_background_cleanup','semantic.adopt_shown_retouch_region') THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.id IS NULL OR t.operation IS DISTINCT FROM p_operation OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu' OR t.state IS DISTINCT FROM 'succeeded' OR t.cancel_requested
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence OR t.canonical_result_id IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'apply' OR r.status IS DISTINCT FROM 'running'
 OR r.cancel_requested OR NOT r.graph_sealed OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN NULL;END IF;
 SELECT id INTO project FROM photo.projects WHERE tenant_id=p_tenant AND id=(r.config_snapshot->>'project_id')::uuid AND archived_at IS NULL;
 IF project IS NULL THEN RETURN NULL;END IF;
 CASE p_operation
 WHEN 'semantic.adopt_shown_exposure' THEN SELECT count(*) INTO matches FROM photo.v27_shown_exposure_adoption_context(p_tenant,p_task) c WHERE c.attempt=p_attempt AND c.fence=p_fence AND c.result_id=t.canonical_result_id AND c.operation=p_operation AND c.operation_version='1';
 WHEN 'semantic.adopt_shown_removal' THEN SELECT count(*) INTO matches FROM photo.v27_shown_removal_adoption_context(p_tenant,p_task) c WHERE c.attempt=p_attempt AND c.fence=p_fence AND c.result_id=t.canonical_result_id AND c.operation=p_operation AND c.operation_version='1';
 WHEN 'semantic.adopt_shown_background_cleanup' THEN SELECT count(*) INTO matches FROM photo.v27_shown_background_cleanup_adoption_context(p_tenant,p_task) c WHERE c.attempt=p_attempt AND c.fence=p_fence AND c.result_id=t.canonical_result_id AND c.operation=p_operation AND c.operation_version='1';
 WHEN 'semantic.adopt_shown_retouch_region' THEN SELECT count(*) INTO matches FROM photo.v27_shown_retouch_adoption_context(p_tenant,p_task) c WHERE c.attempt=p_attempt AND c.fence=p_fence AND c.result_id=t.canonical_result_id AND c.operation=p_operation AND c.operation_version='1';
 ELSE RETURN NULL;
 END CASE;
 IF matches<>1 THEN RETURN NULL;END IF;
 -- Bind the scheduler read to the immutable selected preview, rather than a
 -- running-only worker predicate or request-local tenant context.
 SELECT (node->>'target_id')::uuid INTO STRICT target
 FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') node
 JOIN photo.artifacts params ON params.tenant_id=p_tenant AND params.id=t.params_artifact_id
 WHERE node->>'task_id'=t.id::text AND node->>'params_artifact_id'=params.id::text AND node->>'params_sha'=params.sha256;
 preview_key:=CASE p_operation WHEN 'semantic.adopt_shown_exposure' THEN 'semantic_preapply_preview'
 WHEN 'semantic.adopt_shown_removal' THEN 'semantic_removal_preview'
 WHEN 'semantic.adopt_shown_background_cleanup' THEN 'semantic_background_cleanup_preview'
 WHEN 'semantic.adopt_shown_retouch_region' THEN 'semantic_retouch_preview' END;
 receipt_key:=CASE p_operation WHEN 'semantic.adopt_shown_exposure' THEN 'v27_preapply_preview_receipt'
 WHEN 'semantic.adopt_shown_removal' THEN 'v27_removal_preview_receipt'
 WHEN 'semantic.adopt_shown_background_cleanup' THEN 'v27_background_cleanup_preview_receipt'
 WHEN 'semantic.adopt_shown_retouch_region' THEN 'v27_retouch_preview_receipt' END;
 SELECT (preview.config_snapshot->preview_key->>'session_id')::uuid INTO STRICT session_id
 FROM photo.runs preview JOIN photo.tasks producer ON producer.tenant_id=preview.tenant_id AND producer.run_id=preview.id
 AND producer.id::text=to_jsonb(preview)->receipt_key->>'task_id'
 WHERE preview.tenant_id=p_tenant AND preview.kind='preview' AND preview.graph_sealed AND NOT preview.cancel_requested
 AND preview.created_by=r.created_by AND preview.config_snapshot->preview_key->'params'->>'target_id'=target::text
 AND EXISTS(SELECT 1 FROM photo.semantic_targets target_row JOIN photo.semantic_intents intent
 ON intent.tenant_id=target_row.tenant_id AND intent.id=target_row.intent_id JOIN photo.v2_review_batches batch
 ON batch.tenant_id=target_row.tenant_id AND batch.id=target_row.batch_id
 WHERE target_row.tenant_id=p_tenant AND target_row.id=target AND target_row.project_id=project
 AND target_row.child_run_id=r.id AND target_row.state='queued' AND batch.child_run_id=r.id
 AND batch.actor_id=r.created_by AND batch.action='apply' AND batch.project_id=project
 AND batch.workflow_id=intent.workflow_id AND intent.project_id=project AND intent.snapshot_id=r.input_snapshot_id
 AND preview.config_snapshot->preview_key->'params'->>'intent_id'=intent.id::text)
 AND producer.state='succeeded' AND NOT producer.cancel_requested
 AND producer.canonical_result_id::text=to_jsonb(preview)->receipt_key->>'result_id'
 AND producer.attempt::text=to_jsonb(preview)->receipt_key->>'attempt'
 AND producer.fencing_token::text=to_jsonb(preview)->receipt_key->>'fence'
 AND NOT EXISTS(SELECT 1 FROM unnest(ARRAY['presentation','numerical_evidence','working_image','pixels']) field
 WHERE NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.project_id=project
 AND i.artifact_id::text=to_jsonb(preview)->receipt_key->field->>'artifact_id'
 AND i.sha256=to_jsonb(preview)->receipt_key->field->>'sha256'));
 IF photo.v2_editor_authorized(p_tenant,project,r.created_by,session_id) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN project;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value OR no_data_found OR too_many_rows THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.semantic_shown_adoption_evidence_scope(uuid,uuid,integer,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_adoption_evidence_scope(uuid,uuid,integer,bigint,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_semantic_admission,photo_maintenance;

CREATE FUNCTION photo.v27_shown_adoption_artifact(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_artifact uuid,p_operation text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text,role text,source text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,
 CASE WHEN u.id IS NOT NULL THEN u.role ELSE NULL END,CASE WHEN u.id IS NOT NULL THEN 'output' ELSE 'input' END
 FROM photo.tasks t
 CROSS JOIN LATERAL (SELECT photo.semantic_shown_adoption_evidence_scope(p_tenant,p_task,p_attempt,p_fence,p_operation) AS project) scope
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=p_artifact
 LEFT JOIN photo.output_uploads u ON u.tenant_id=t.tenant_id AND u.task_id=t.id AND u.artifact_id=a.id
 AND u.attempt=p_attempt AND u.fence=p_fence AND u.state='ready' AND u.completed_at IS NOT NULL
 AND a.created_by_task_id=t.id AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence
 AND a.sha256=u.sha256 AND a.bytes=u.bytes AND a.bucket=u.bucket AND a.object_key=u.object_key
 AND a.object_version=u.object_version AND a.media_type=u.media_type AND a.output_role=u.role
 LEFT JOIN photo.v2_task_inputs i ON i.tenant_id=t.tenant_id AND i.project_id=scope.project
 AND i.task_id=t.id AND i.artifact_id=a.id AND i.sha256=a.sha256
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND scope.project IS NOT NULL
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 1 AND 9007199254740991
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND (u.id IS NOT NULL OR i.id IS NOT NULL);
$$;
ALTER FUNCTION photo.v27_shown_adoption_artifact(uuid,uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_shown_adoption_artifact(uuid,uuid,integer,bigint,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_semantic_admission,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v27_shown_adoption_artifact(uuid,uuid,integer,bigint,uuid,text) TO photo_scheduler;

CREATE FUNCTION photo.v27_shown_adoption_staged_outputs(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_operation text)
RETURNS TABLE(artifact_id uuid,sha256 text,state text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT u.artifact_id,u.sha256,u.state FROM photo.output_uploads u
 WHERE u.tenant_id=p_tenant AND u.task_id=p_task AND u.attempt=p_attempt AND u.fence=p_fence
 AND photo.semantic_shown_adoption_evidence_scope(p_tenant,p_task,p_attempt,p_fence,p_operation) IS NOT NULL;
$$;
ALTER FUNCTION photo.v27_shown_adoption_staged_outputs(uuid,uuid,integer,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_shown_adoption_staged_outputs(uuid,uuid,integer,bigint,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_semantic_admission,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v27_shown_adoption_staged_outputs(uuid,uuid,integer,bigint,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
