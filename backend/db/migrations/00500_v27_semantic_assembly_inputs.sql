-- +goose Up
-- All frozen transfers must be canonically adopted before CPU assembly staging.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_assembly_inputs(p_tenant uuid,p_run uuid)
RETURNS TABLE(ordinal integer,task_id uuid,artifact_id uuid,sha256 text)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; nodes jsonb; node jsonb; n integer:=0;
 producer photo.tasks%ROWTYPE; result photo.task_results%ROWTYPE; params photo.artifacts%ROWTYPE; root uuid; digest text; h bytea; expected uuid;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 nodes:=r.config_snapshot->'v27_semantic'->'nodes';
 IF r.id IS NULL OR w.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR NOT r.graph_sealed OR r.cancel_requested
 OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2' OR w.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM w.project_id::text
 OR r.config_snapshot->>'input_snapshot_id' IS DISTINCT FROM r.input_snapshot_id::text
 OR jsonb_typeof(nodes) IS DISTINCT FROM 'array' THEN RETURN;END IF;
 IF jsonb_array_length(nodes) NOT BETWEEN 1 AND 5000
 OR jsonb_array_length(nodes)<>(SELECT item_count FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id)
 OR NOT photo.v25_lens_snapshot_ready(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,
  (r.config_snapshot->'v27_semantic'->>'auth_session_id')::uuid) THEN RETURN;END IF;
 h:=public.digest(uuid_send(r.id)||convert_to('workflow','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF w.id IS DISTINCT FROM expected THEN RETURN;END IF;
 -- Validate the whole set first; never expose a partially ready expansion.
 FOR node IN SELECT value FROM jsonb_array_elements(nodes) LOOP
  h:=public.digest(uuid_send(w.id)||convert_to('semantic.transfer:'||n::text,'UTF8'),'sha1');
  h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
  expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
  IF (node->>'task_id')::uuid IS DISTINCT FROM expected THEN RETURN;END IF;
  SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(node->>'task_id')::uuid;
  IF producer.id IS NULL OR producer.node_key<>'semantic.transfer:'||n::text
   OR producer.operation<>'semantic.transfer' OR producer.operation_version<>'1' OR producer.resource_class<>'gpu'
   OR producer.params_artifact_id::text IS DISTINCT FROM node->>'params_artifact_id' THEN RETURN;END IF;
  SELECT * INTO params FROM photo.artifacts WHERE tenant_id=p_tenant AND id=producer.params_artifact_id;
  IF params.sha256 IS DISTINCT FROM node->>'params_sha' OR params.state<>'ready' OR params.verified_at IS NULL
   OR params.media_type<>'application/json' OR params.object_version IS NULL OR params.object_version IN('','null')
   OR (params.expires_at IS NOT NULL AND params.expires_at<=clock_timestamp()) OR NOT photo.v2_input_closure_ready(producer.id)
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=producer.id
    AND i.artifact_id=params.id AND i.sha256=params.sha256 AND i.contract='Params') THEN RETURN;END IF;
  SELECT z.* INTO result FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.id=producer.canonical_result_id AND z.task_id=producer.id;
  IF jsonb_typeof(result.outputs) IS DISTINCT FROM 'array' THEN RETURN;END IF;
  IF jsonb_array_length(result.outputs)<>1 THEN RETURN;END IF;
  root:=(result.outputs->0->>'artifact_id')::uuid;digest:=result.outputs->0->>'sha256';
  IF NOT photo.v27_semantic_candidate_scope(p_tenant,w.id,root,digest) THEN RETURN;END IF;
  n:=n+1;
 END LOOP;
 RETURN QUERY SELECT (item.n-1)::integer,t.id,(z.outputs->0->>'artifact_id')::uuid,z.outputs->0->>'sha256'
 FROM jsonb_array_elements(nodes) WITH ORDINALITY item(node,n)
 JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=(item.node->>'task_id')::uuid
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 ORDER BY item.n;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN;
END $$;
ALTER FUNCTION photo.v27_semantic_assembly_inputs(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_assembly_inputs(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_assembly_inputs(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
