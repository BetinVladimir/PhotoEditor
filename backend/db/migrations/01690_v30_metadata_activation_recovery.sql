-- +goose Up
-- Recover exact activation without registering inputs, dispatching or reserving again.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_task_activated(p_tenant uuid,p_expansion uuid,p_token uuid,p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body text;pin text;proof jsonb;e photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;
 ref jsonb;expected jsonb;d photo.backend_documents%ROWTYPE;r photo.runs%ROWTYPE;approved text[];
BEGIN
 IF jsonb_typeof(p_task) IS DISTINCT FROM 'object' OR octet_length(p_task::text)>1048576 THEN RETURN false;END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion;
 IF NOT FOUND OR e.kind<>'v30.delivery.metadata/1' OR e.state<>'closed' OR e.cursor<>'1'
 OR e.expected_count<>1 OR p_token IS NULL OR e.expansion_token IS DISTINCT FROM p_token
 OR e.v30_metadata_proof_jcs IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=e.run_id;
 IF NOT FOUND OR r.kind<>'v2' OR r.status NOT IN('running','waiting_review','completed')
 OR r.cancel_requested OR NOT r.graph_sealed THEN RETURN false;END IF;
 proof:=e.v30_metadata_proof_jcs::jsonb;
 IF proof->'binding' IS DISTINCT FROM r.config_snapshot->'v30_delivery_export'->'binding'
 OR proof->>'policy' IS DISTINCT FROM ((r.config_snapshot->'v30_delivery_export'->>'config_jcs')::jsonb)->>'metadata_policy'
 OR proof->>'task_id' IS DISTINCT FROM photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),'metadata.write:0')::text THEN RETURN false;END IF;
 IF proof->>'policy'='approved_exif' THEN
  IF jsonb_typeof(r.config_snapshot->'v30_metadata_approved_fields') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  SELECT coalesce(array_agg(value ORDER BY value COLLATE "C"),ARRAY[]::text[]) INTO approved
  FROM jsonb_array_elements_text(r.config_snapshot->'v30_metadata_approved_fields');
 ELSE approved:=ARRAY[]::text[];END IF;
 IF proof->'approved_fields' IS DISTINCT FROM to_jsonb(approved)
 OR photo.v30_metadata_write_plan_valid((proof->>'task_id')::uuid,proof->'encoded',proof->>'encoded_jcs',
 proof->>'frozen_jcs',proof->>'frozen_sha256',proof->'binding',proof->'metadata_source',proof->>'metadata_jcs',
 proof->>'policy',approved,proof->>'projection_jcs',proof->>'params_jcs') IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=e.run_id AND id=(proof->>'task_id')::uuid FOR SHARE;
 IF NOT FOUND OR t.state NOT IN('ready','leased','running','succeeded','failed','cancelled','skipped') OR t.cancel_requested OR t.operation<>'metadata.write'
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
 RETURN EXISTS(SELECT 1 FROM photo.tasks predecessor WHERE predecessor.tenant_id=p_tenant
 AND predecessor.id=e.parent_task_id AND predecessor.run_id=e.run_id AND predecessor.operation='export.encode'
 AND predecessor.state='succeeded' AND NOT predecessor.cancel_requested AND predecessor.canonical_result_id IS NOT NULL
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(predecessor.id,predecessor.canonical_result_id))
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-encode-verified'
 AND event_id=photo.v2_adoption_event_id(predecessor.id,predecessor.canonical_result_id)));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_task_activated(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_task_activated(uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_metadata_task_activated(uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
