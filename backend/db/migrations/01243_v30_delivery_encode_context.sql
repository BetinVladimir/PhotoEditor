-- +goose Up
-- Delivery children have no standalone v2_workflow and encode has two outputs.
-- This read context is not adoption authority or permission to publish an export.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_encode_context(p_tenant uuid,p_task uuid)
RETURNS TABLE(attempt integer,fence bigint,result_id uuid,operation text,operation_version text,
 root_artifact_id uuid,root_sha256 text,params_artifact_id uuid,params_sha256 text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;e photo.graph_expansions%ROWTYPE;
 z photo.task_results%ROWTYPE;b photo.backend_documents%ROWTYPE;request jsonb;manifest photo.artifacts%ROWTYPE;
 slot integer;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND OR t.operation<>'export.encode' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.state<>'succeeded' OR t.cancel_requested OR t.canonical_result_id IS NULL
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR NOT r.graph_sealed OR r.cancel_requested
 OR NOT (r.config_snapshot ? 'v30_delivery_export') THEN RETURN;END IF;
 request:=r.config_snapshot->'v30_delivery_export';
 slot:=(request->>'slot_index')::integer;
 IF slot NOT BETWEEN 0 AND 79999 OR r.id<>photo.v30_uuid5((request->>'parent_run_id')::uuid,'child:export:'||slot::text)
 OR t.id<>photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),'export.encode:0') THEN RETURN;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant
 AND run_id=(request->>'parent_run_id')::uuid AND kind='v2.delivery/1' AND state='open';
 IF NOT FOUND OR slot>=e.cursor::integer OR photo.v30_delivery_expansion_ready(e.id) IS DISTINCT FROM true THEN RETURN;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.delivery_items i JOIN photo.delivery_packages d
 ON d.tenant_id=i.tenant_id AND d.id=i.package_id JOIN photo.v2_workflows w
 ON w.tenant_id=d.tenant_id AND w.id=d.workflow_id AND w.run_id=e.run_id
 JOIN photo.exports x ON x.tenant_id=i.tenant_id AND x.id=i.export_id
 WHERE i.tenant_id=p_tenant AND i.ordinal=(request->>'ordinal')::integer
 AND i.preset_id=(request->'preset'->>'preset_id')::uuid AND i.state='pending' AND d.state='building'
 AND x.id=(r.config_snapshot->>'export_id')::uuid AND x.task_id=t.id AND x.status='queued'
 AND x.asset_version_id=(request->'binding'->>'asset_version_id')::uuid
 AND r.config_snapshot->>'project_id'=w.project_id::text) THEN RETURN;END IF;
 IF EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=r.id AND state<>'closed')
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=t.id AND id=t.canonical_result_id;
 IF NOT FOUND OR z.attempt<>t.attempt OR z.fencing_token<>t.fencing_token
 OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(z.outputs)<>2 THEN RETURN;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(z.outputs) output WHERE NOT EXISTS(
 SELECT 1 FROM photo.artifacts a JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
 WHERE a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid AND a.sha256=output->>'sha256'
 AND a.output_role IN('encoded_image','encoded_manifest') AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND u.task_id=t.id AND u.attempt=t.attempt AND u.fence=t.fencing_token AND u.state='ready'
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND a.sha256=u.sha256 AND a.output_role=u.role)) THEN RETURN;END IF;
 IF (SELECT count(DISTINCT a.output_role) FROM jsonb_array_elements(z.outputs) output JOIN photo.artifacts a
 ON a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid AND a.sha256=output->>'sha256')<>2 THEN RETURN;END IF;
 SELECT a.* INTO manifest FROM jsonb_array_elements(z.outputs) output JOIN photo.artifacts a
 ON a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid AND a.sha256=output->>'sha256'
 WHERE a.output_role='encoded_manifest' AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO b FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=e.run_id AND owner_id=t.id
 AND purpose='task_params' AND artifact_id=t.params_artifact_id AND state='ready';
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.artifacts a
 JOIN photo.v2_task_inputs i ON i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=a.id AND i.sha256=a.sha256
 WHERE a.tenant_id=p_tenant AND a.id=b.artifact_id AND a.sha256=b.sha256 AND a.media_type='application/json'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 2 AND 65536
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND i.contract='Params') THEN RETURN;END IF;
 RETURN QUERY SELECT t.attempt,t.fencing_token,z.id,t.operation,t.operation_version,manifest.id,manifest.sha256,b.artifact_id,b.sha256;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_delivery_encode_context(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_encode_context(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_encode_context(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
