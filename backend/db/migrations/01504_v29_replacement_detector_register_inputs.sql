-- +goose Up
-- Full mapped physical locks are retained by the dedicated Go caller.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_replacement_detector_inputs_ready(p_tenant uuid,p_run uuid,p_proof jsonb,p_witness jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE descriptor jsonb;image jsonb;ref jsonb;current_object jsonb;role text;idx integer:=0;owner uuid;
BEGIN
 descriptor:=(p_proof->>'descriptor_jcs')::jsonb;owner:=(descriptor->>'task_id')::uuid;
 IF photo.read_v29_replacement_detector_transport_admission(p_tenant,p_run,owner,p_proof) IS NULL
 OR jsonb_typeof(p_witness) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_witness) k) IS DISTINCT FROM ARRAY['actor_id','arrays','auth_session_id','schema_version','snapshot_id','source','task_id']::text[]
 OR p_witness->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_witness->>'task_id' IS DISTINCT FROM owner::text
 OR p_witness->>'snapshot_id' IS DISTINCT FROM p_proof->>'snapshot_id'
 OR p_witness->>'actor_id' IS DISTINCT FROM descriptor->>'actor_id'
 OR p_witness->>'auth_session_id' IS DISTINCT FROM descriptor->>'auth_session_id'
 OR jsonb_typeof(p_witness->'arrays') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 ref:=descriptor->'base'->'source';
 current_object:=photo.v29_replacement_detector_input_object(p_tenant,p_run,owner,'WorkingImage',(ref->>'artifact_id')::uuid,ref->>'sha256',p_proof);
 IF current_object IS NULL OR p_witness->'source' IS DISTINCT FROM current_object THEN RETURN false;END IF;
 image:=(descriptor->>'source_jcs')::jsonb;
 FOR role,ref IN SELECT x.role,x.ref FROM (VALUES (0,'WorkingPixels'::text,image->'pixels'),(1,'WorkingAlpha'::text,image->'alpha')) x(n,role,ref)
 WHERE x.ref IS NOT NULL AND x.ref<>'null'::jsonb ORDER BY n LOOP
  current_object:=photo.v29_replacement_detector_input_object(p_tenant,p_run,owner,role,(ref->>'artifact_id')::uuid,ref->>'sha256',p_proof);
  IF current_object IS NULL OR p_witness->'arrays'->idx IS DISTINCT FROM jsonb_build_object('role',role,'object',current_object)
   OR current_object->>'bucket' IS DISTINCT FROM p_witness->'source'->>'bucket' THEN RETURN false;END IF;
  idx:=idx+1;
 END LOOP;
 RETURN idx BETWEEN 1 AND 2 AND jsonb_array_length(p_witness->'arrays')=idx;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_replacement_detector_inputs_ready(uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_replacement_detector_inputs_ready(uuid,uuid,jsonb,jsonb) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_portal,photo_gateway;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.register_v29_replacement_detector_inputs(p_tenant uuid,p_run uuid,p_proof jsonb,p_closure jsonb,p_witness jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;descriptor jsonb;saved jsonb;expected jsonb;actual jsonb;owner uuid;project uuid;entry_count integer;
BEGIN
 IF photo.v29_replacement_detector_inputs_ready(p_tenant,p_run,p_proof,p_witness) IS DISTINCT FROM true THEN RETURN false;END IF;
 descriptor:=(p_proof->>'descriptor_jcs')::jsonb;owner:=(descriptor->>'task_id')::uuid;project:=(descriptor->>'project_id')::uuid;
 saved:=photo.read_v29_replacement_detector_transport_admission(p_tenant,p_run,owner,p_proof);
 IF saved IS NULL OR saved IS DISTINCT FROM p_closure THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 PERFORM id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND id=photo.v30_uuid5(p_run,'workflow')
 AND project_id=project AND input_snapshot_id=r.input_snapshot_id AND workflow_type='preview' AND renderer_version='v2.2'
 AND r.v29_replacement_detector_execution_policy IS NOT NULL
 AND policy_sha=encode(sha256(convert_to(r.v29_replacement_detector_execution_policy,'UTF8')),'hex') FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 entry_count:=(saved->>'entry_count')::integer;
 expected:=saved->'entries'||jsonb_build_array(jsonb_build_object('contract','InputClosureChunk','ref',saved->'chunks'->0),jsonb_build_object('contract','InputClosureManifest','ref',saved->'manifest'));
 IF (SELECT count(DISTINCT v->'ref'->>'artifact_id') FROM jsonb_array_elements(expected) v)<>jsonb_array_length(expected) THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('contract',i.contract,'ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256)) ORDER BY i.ordinal)
 INTO actual FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=owner;
 IF actual IS NOT NULL THEN
  RETURN actual=expected AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=owner
   AND (i.project_id<>project OR i.ordinal IS DISTINCT FROM CASE WHEN i.contract='InputClosureChunk' THEN 1000000 WHEN i.contract='InputClosureManifest' THEN 1000001 ELSE
   (SELECT (n-1)::integer FROM jsonb_array_elements(saved->'entries') WITH ORDINALITY x(v,n) WHERE v->'ref'->>'artifact_id'=i.artifact_id::text) END));
 END IF;
 INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal)
 SELECT p_tenant,project,owner,(v->'ref'->>'artifact_id')::uuid,v->'ref'->>'sha256',v->>'contract',
 CASE WHEN n<=entry_count THEN (n-1)::integer ELSE 1000000+(n-entry_count-1)::integer END
 FROM jsonb_array_elements(expected) WITH ORDINALITY x(v,n);
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.register_v29_replacement_detector_inputs(uuid,uuid,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v29_replacement_detector_inputs(uuid,uuid,jsonb,jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.register_v29_replacement_detector_inputs(uuid,uuid,jsonb,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
