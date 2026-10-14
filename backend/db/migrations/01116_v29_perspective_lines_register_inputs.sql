-- +goose Up
-- Register a complete typed closure only; no task/profile mutation or dispatch.
-- A dedicated V2 workflow with trusted policy must already exist.
-- +goose StatementBegin
CREATE FUNCTION photo.register_v29_perspective_lines_inputs(p_tenant uuid,p_run uuid,p_proof jsonb,p_witness jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;c photo.backend_documents%ROWTYPE;m photo.backend_documents%ROWTYPE;
 plan jsonb;image jsonb;chunk jsonb;manifest jsonb;expected jsonb;actual jsonb;row jsonb;role text;ref jsonb;
 project uuid;task uuid;count_entries integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_proof IS NULL OR jsonb_typeof(p_proof) IS DISTINCT FROM 'object' OR pg_column_size(p_proof)>262144
 OR photo.v29_perspective_lines_inputs_ready(p_tenant,p_run,p_witness) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 plan:=r.config_snapshot->'v29_perspective_line_detection';project:=(plan->>'project_id')::uuid;task:=(plan->>'task_id')::uuid;
 image:=(plan->>'source_jcs')::jsonb;
 PERFORM id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND project_id=project
 AND input_snapshot_id=r.input_snapshot_id AND workflow_type='preview' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=task FOR UPDATE;
 IF NOT FOUND OR t.state<>'blocked' OR t.cancel_requested THEN RETURN false;END IF;
 SELECT * INTO c FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=task AND purpose='v2_input_chunk_0' AND state='ready' FOR UPDATE;
 IF NOT FOUND OR c.v29_perspective_lines_transport IS NULL THEN RETURN false;END IF;
 SELECT * INTO m FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=task AND purpose='v2_input_manifest' AND state='ready' FOR UPDATE;
 IF NOT FOUND OR m.v29_perspective_lines_transport IS NULL THEN RETURN false;END IF;
 -- Current input gate independently reconstructed both control bodies and
 -- verified their ready artifact locators before accepting IO witnesses.
 chunk:=c.v29_perspective_lines_transport::jsonb;manifest:=m.v29_perspective_lines_transport::jsonb;
 count_entries:=jsonb_array_length(chunk->'entries');
 IF p_proof IS DISTINCT FROM jsonb_build_object('schema_version',1,'task_id',task,
 'entries',chunk->'entries','entry_count',count_entries,'entries_sha',manifest->>'entries_sha',
 'chunks',jsonb_build_array(jsonb_build_object('artifact_id',c.artifact_id,'sha256',c.sha256)),
 'manifest',jsonb_build_object('artifact_id',m.artifact_id,'sha256',m.sha256),
 'chunk_jcs',c.v29_perspective_lines_transport,'manifest_jcs',m.v29_perspective_lines_transport) THEN RETURN false;END IF;
 -- Every historical dense occurrence must still be an authorized source child,
 -- even though the detector reads only current RGB/alpha pixels.
 FOR role,ref IN SELECT x.role,x.ref FROM (
  SELECT 'WorkingPixels'::text role,image->'pixels' ref
  UNION ALL SELECT 'WorkingAlpha',image->'alpha' WHERE image->'alpha'<>'null'::jsonb
  UNION ALL SELECT 'InverseField/'||(n-1)::text,v->'inverse_field'
   FROM jsonb_array_elements(image->'transform_chain'->'steps') WITH ORDINALITY a(v,n) WHERE v->>'kind'='dense_warp'
 ) x LOOP
  IF photo.lock_v29_perspective_source_child(p_tenant,project,r.input_snapshot_id,r.created_by,
   (plan->>'auth_session_id')::uuid,plan->'base',plan->>'source_jcs',(ref->>'artifact_id')::uuid,ref->>'sha256',role) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 expected:=chunk->'entries'||jsonb_build_array(
 jsonb_build_object('contract','InputClosureChunk','ref',jsonb_build_object('artifact_id',c.artifact_id,'sha256',c.sha256)),
 jsonb_build_object('contract','InputClosureManifest','ref',jsonb_build_object('artifact_id',m.artifact_id,'sha256',m.sha256)));
 IF (SELECT count(DISTINCT v->'ref'->>'artifact_id') FROM jsonb_array_elements(expected) v)<>jsonb_array_length(expected) THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('contract',i.contract,'ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256)) ORDER BY i.ordinal)
 INTO actual FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=task;
 IF actual IS NOT NULL THEN
  RETURN actual=expected AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=task
  AND (i.project_id<>project OR i.ordinal NOT BETWEEN 0 AND count_entries-1 AND i.ordinal NOT IN(1000000,1000001)));
 END IF;
 -- All checks precede mutation. A constraint error aborts the caller's
 -- transaction; no partial successful registration is returned.
 INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal)
 SELECT p_tenant,project,task,(v->'ref'->>'artifact_id')::uuid,v->'ref'->>'sha256',v->>'contract',
 CASE WHEN n<=count_entries THEN (n-1)::integer ELSE 1000000+(n-count_entries-1)::integer END
 FROM jsonb_array_elements(expected) WITH ORDINALITY a(v,n);
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.register_v29_perspective_lines_inputs(uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v29_perspective_lines_inputs(uuid,uuid,jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.register_v29_perspective_lines_inputs(uuid,uuid,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
