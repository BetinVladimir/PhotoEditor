-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.register_v29_negative_replacement_transport(p_tenant uuid,p_run uuid,p_descriptor text,p_proof jsonb,p_transport jsonb,p_manifest_body text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE plan jsonb;request jsonb;saved jsonb;profile jsonb;entries jsonb;task uuid;n integer;position integer;batch jsonb;matched integer;
 d photo.backend_documents%ROWTYPE;body jsonb;roots jsonb;actual jsonb;expected jsonb;chunk jsonb;counter integer:=0;
BEGIN
 IF p_descriptor IS NULL OR octet_length(p_descriptor) NOT BETWEEN 2 AND 1048576
 OR p_manifest_body IS NULL OR octet_length(p_manifest_body) NOT BETWEEN 2 AND 1048576
 OR pg_column_size(p_transport)>65536
 OR jsonb_typeof(p_transport) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_transport) k) IS DISTINCT FROM ARRAY['chunks','count','manifest','sha256']::text[]
 OR photo.read_v29_negative_replacement_published_transport(p_tenant,p_run,(p_descriptor::jsonb->'task'->>'id')::uuid,p_descriptor,p_proof) IS DISTINCT FROM p_transport THEN RETURN false;END IF;
 plan:=p_descriptor::jsonb;entries:=plan->'entries';task:=(plan->'task'->>'id')::uuid;n:=jsonb_array_length(entries);
 SELECT (config_snapshot->>'v29_negative_replacement_draft_jcs')::jsonb INTO request FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 saved:=((request->>'recovery_jcs')::jsonb->>'descriptor_jcs')::jsonb;profile:=(saved->>'profile_jcs')::jsonb;
 body:=p_manifest_body::jsonb;
 IF p_transport->'count' IS DISTINCT FROM to_jsonb(n) OR p_transport->'chunks' IS DISTINCT FROM body->'chunks'
 OR p_transport->>'sha256' IS DISTINCT FROM body->>'entries_sha'
 OR photo.v29_negative_replacement_transport_document_scope(p_tenant,p_run,task,'v2_input_manifest',
 p_transport->'manifest'->>'sha256',octet_length(p_manifest_body),p_proof,p_manifest_body) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=task AND purpose='v2_input_manifest'
 AND state='ready' AND artifact_id=(p_transport->'manifest'->>'artifact_id')::uuid AND sha256=p_transport->'manifest'->>'sha256' FOR SHARE;
 IF d.id IS NULL OR d.bytes IS DISTINCT FROM octet_length(p_manifest_body)::bigint
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket
 AND a.object_version=d.object_version AND a.object_version NOT IN('','null') AND a.media_type='application/json' AND a.output_role=d.purpose
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a) THEN RETURN false;END IF;
 -- Only Params/source/report/calibration are direct wire roots. Source arrays remain
 -- in the complete frozen closure and registered task inputs.
 SELECT jsonb_agg(ref ORDER BY (ref->>'artifact_id') COLLATE "C") INTO roots FROM (
 SELECT jsonb_build_object('artifact_id',plan->'task'->>'params_artifact_id','sha256',plan->'task'->>'params_sha') ref
 UNION ALL SELECT (saved->>'params_jcs')::jsonb->'base'->'source'
 UNION ALL SELECT profile->'negative_qualification'->'report'
 UNION ALL SELECT profile->'negative_qualification'->'calibration') refs;
 expected:=entries;
 FOR chunk IN SELECT value FROM jsonb_array_elements(p_transport->'chunks') LOOP
 expected:=expected||jsonb_build_array(jsonb_build_object('contract','InputClosureChunk','ref',chunk));counter:=counter+1;
 END LOOP;
 expected:=expected||jsonb_build_array(jsonb_build_object('contract','InputClosureManifest','ref',p_transport->'manifest'));
 SELECT jsonb_agg(jsonb_build_object('contract',i.contract,'ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256)) ORDER BY i.ordinal)
 INTO actual FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=task;
 IF photo.v2_input_closure_ready(task) THEN
 RETURN actual=expected AND EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=task
 AND t.inputs=jsonb_build_array(p_transport->'manifest')||roots
 AND t.execution_profile->'v2_input_manifest'=p_transport->'manifest'
 AND t.execution_profile->'v2_entry_count'=to_jsonb(n) AND t.execution_profile->>'v2_entries_sha'=p_transport->>'sha256');END IF;
 IF actual IS NOT NULL THEN RETURN false;END IF;
 position:=0;
 WHILE position<n LOOP
 SELECT jsonb_agg(value ORDER BY ord) INTO batch FROM jsonb_array_elements(entries) WITH ORDINALITY a(value,ord) WHERE ord>position AND ord<=position+512;
 matched:=photo.register_v2_task_input_batch_pre_relight(p_tenant,p_run,task,position,batch);
 IF matched IS DISTINCT FROM jsonb_array_length(batch) THEN RAISE EXCEPTION 'NEGATIVE_BATCH_CHANGED' USING ERRCODE='23514';END IF;
 position:=position+matched;
 END LOOP;
 IF photo.seal_v2_task_inputs_pre_relight(p_tenant,p_run,task,p_transport->'manifest',p_transport->'chunks',n,p_transport->>'sha256',roots) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'NEGATIVE_INPUT_SEAL_FAILED' USING ERRCODE='23514';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.register_v29_negative_replacement_transport(uuid,uuid,text,jsonb,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v29_negative_replacement_transport(uuid,uuid,text,jsonb,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_v29_negative_replacement_transport(uuid,uuid,text,jsonb,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
