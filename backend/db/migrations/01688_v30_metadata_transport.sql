-- +goose Up
-- Exact registered metadata inputs; no registration or dispatch in this predicate.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_transport_ready(p_tenant uuid,p_expansion uuid,p_token uuid,p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body text;pin text;proof jsonb;e photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;
 ref jsonb;expected jsonb;d photo.backend_documents%ROWTYPE;
BEGIN
 IF jsonb_typeof(p_task) IS DISTINCT FROM 'object' OR octet_length(p_task::text)>1048576 THEN RETURN false;END IF;
 SELECT proof_jcs,proof_sha INTO body,pin FROM photo.read_v30_delivery_metadata_proof(p_tenant,p_expansion,p_token);
 IF body IS NULL THEN RETURN false;END IF;
 proof:=body::jsonb;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=e.run_id AND id=(proof->>'task_id')::uuid FOR SHARE;
 IF NOT FOUND OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested OR t.operation<>'metadata.write'
 OR t.operation_version<>'1' OR t.resource_class<>'cpu' OR t.node_key<>'metadata.write:0'
 OR t.id::text IS DISTINCT FROM p_task->>'id' OR t.node_key IS DISTINCT FROM p_task->>'node_key'
 OR t.operation IS DISTINCT FROM p_task->>'operation' OR t.operation_version IS DISTINCT FROM p_task->>'operation_version'
 OR t.resource_class IS DISTINCT FROM p_task->>'resource_class'
 OR t.params_artifact_id::text IS DISTINCT FROM p_task->>'params_artifact_id'
 OR t.params_artifact_id::text IS DISTINCT FROM proof->'params'->>'artifact_id'
 OR p_task->>'params_sha' IS DISTINCT FROM proof->'params'->>'sha256'
 OR t.input_digest IS DISTINCT FROM p_task->>'input_digest'
 OR photo.v30_delivery_profile_matches(t.execution_profile,p_task->'execution_profile') IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 expected:=jsonb_build_array(proof->'params',proof->'encoded',(proof->>'encoded_jcs')::jsonb->'artifact',proof->'projection');
 IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=t.id
 AND contract NOT IN('InputClosureChunk','InputClosureManifest'))<>4 THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(expected) LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a
  ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
  WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=(ref->>'artifact_id')::uuid
  AND i.sha256=ref->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND (ref<>proof->'params' OR i.contract='Params')) THEN RETURN false;END IF;
 END LOOP;
 FOR ref IN SELECT value FROM jsonb_array_elements(jsonb_build_array(proof->'params',proof->'projection')) LOOP
  SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=e.run_id AND owner_id=t.id
  AND artifact_id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256' AND state='ready'
  AND purpose=CASE WHEN ref=proof->'params' THEN 'task_params' ELSE 'v30_metadata_projection' END;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 IF NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=e.run_id
 AND prerequisite_id=e.parent_task_id AND dependent_id=t.id AND condition='success') THEN RETURN false;END IF;
 RETURN e.expansion_lease_until>clock_timestamp();
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_transport_ready(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_transport_ready(uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_metadata_transport_ready(uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
