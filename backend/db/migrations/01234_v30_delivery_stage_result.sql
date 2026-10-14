-- +goose Up
-- Read-only producer discovery; this is not adoption or cursor advancement.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_stage_result(p_expansion uuid,p_token uuid,p_slot integer,p_operation text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 z photo.task_results%ROWTYPE;task_id uuid;outputs jsonb;cfg jsonb;
BEGIN
 IF p_operation IS NULL OR p_operation NOT IN('derivative.resize','derivative.sharpen') THEN RETURN NULL;END IF;
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF expected IS NULL THEN RETURN NULL;END IF;
 cfg:=(expected->>'config_jcs')::jsonb;
 IF (p_operation='derivative.resize' AND cfg->'size' IS NULL)
 OR (p_operation='derivative.sharpen' AND cfg->'sharpen' IS NULL) THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(expected->>'child_run_id')::uuid FOR SHARE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested
 OR r.config_snapshot->'v30_delivery_export' IS DISTINCT FROM expected
 OR NOT EXISTS(SELECT 1 FROM photo.delivery_items i JOIN photo.delivery_packages d ON d.tenant_id=i.tenant_id AND d.id=i.package_id
 JOIN photo.v2_workflows w ON w.tenant_id=d.tenant_id AND w.id=d.workflow_id AND w.run_id=e.run_id
 WHERE i.tenant_id=e.tenant_id
 AND i.ordinal=(expected->>'ordinal')::integer AND i.preset_id=(expected->'preset'->>'preset_id')::uuid
 AND i.export_id=(r.config_snapshot->>'export_id')::uuid AND i.state='pending') THEN RETURN NULL;END IF;
 task_id:=photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),p_operation||':0');
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=task_id FOR SHARE;
 IF NOT FOUND OR t.operation<>p_operation OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.state<>'succeeded' OR t.cancel_requested OR t.canonical_result_id IS NULL
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN NULL;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=e.tenant_id AND task_id=t.id AND id=t.canonical_result_id;
 IF NOT FOUND OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 -- Every declared output must have exact ready upload/producer/fence evidence.
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(z.outputs) output
 WHERE NOT EXISTS(SELECT 1 FROM photo.v2_adoption_artifact(e.tenant_id,t.id,t.attempt,t.fencing_token,(output->>'artifact_id')::uuid) a
 WHERE a.source='output' AND a.sha256=output->>'sha256')) THEN RETURN NULL;END IF;
 SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',a.id,'sha256',a.sha256),
 'role',a.output_role,'bucket',a.bucket,'object_key',a.object_key,'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type) ORDER BY a.id)
 INTO outputs FROM jsonb_array_elements(z.outputs) output JOIN photo.artifacts a
 ON a.tenant_id=e.tenant_id AND a.id=(output->>'artifact_id')::uuid AND a.sha256=output->>'sha256';
 IF (SELECT count(*) FROM jsonb_array_elements(outputs) x WHERE x->>'role'='working_image')<>1
 OR (SELECT count(*) FROM jsonb_array_elements(outputs) x WHERE x->>'role'='derivative_report')<>1
 OR (SELECT count(*) FROM jsonb_array_elements(outputs) x WHERE x->>'role'='working_rgb')<>1
 OR (SELECT count(*) FROM jsonb_array_elements(outputs) x WHERE x->>'role'='working_alpha')>1
 OR jsonb_array_length(outputs) NOT BETWEEN 3 AND 4
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(outputs) x WHERE x->>'role' NOT IN('working_image','derivative_report','working_rgb','working_alpha')) THEN RETURN NULL;END IF;
 IF e.expansion_lease_until<=clock_timestamp() THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('task_id',t.id,'result_id',z.id,'attempt',t.attempt,'fence',t.fencing_token,'outputs',outputs);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_stage_result(uuid,uuid,integer,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_stage_result(uuid,uuid,integer,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_stage_result(uuid,uuid,integer,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
