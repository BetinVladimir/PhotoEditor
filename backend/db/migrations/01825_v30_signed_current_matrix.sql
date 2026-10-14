-- +goose Up
-- Lease-independent current Delivery matrix for signed assembly. This is a
-- private source predicate, not a worker grant or activation transition.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signed_current_matrix(p_tenant uuid,p_run uuid,p_task uuid,
 p_params uuid,p_sha text,p_json text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;proof jsonb;frozen jsonb;snapshot jsonb;assembly jsonb;presets jsonb;
 ref jsonb;shard jsonb;preset jsonb;cfg jsonb;original jsonb;request jsonb;observation jsonb;
 expected jsonb;item jsonb;raw text;child uuid;reason text;workflow uuid;package uuid;
 preset_count integer;item_count integer;slot_count integer;shard_index integer:=0;slot_index integer:=0;
 ordinal integer;document_bytes bigint;input_count bigint;
BEGIN
 source:=photo.v30_delivery_signed_runtime_documents(p_tenant,p_run,p_task,p_params,p_sha,p_json);
 IF source IS NULL OR photo.v30_delivery_signed_current_receipts(p_tenant,p_run,p_task,p_params,p_sha,p_json)
  IS DISTINCT FROM true OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 workflow:=photo.v30_uuid5(p_run,'workflow');package:=(source->>'package_id')::uuid;
 proof:=source->'proof';frozen:=(proof->>'frozen_input_jcs')::jsonb;
 snapshot:=(proof->>'snapshot_jcs')::jsonb;assembly:=(source->>'assembly_jcs')::jsonb;
 SELECT jsonb_agg(value ORDER BY value->>'preset_id') INTO presets
  FROM jsonb_array_elements(frozen->'presets');
 preset_count:=jsonb_array_length(presets);item_count:=jsonb_array_length(snapshot->'items');
 slot_count:=preset_count*item_count;
 IF preset_count NOT BETWEEN 1 AND 16 OR item_count NOT BETWEEN 1 AND 5000
  OR slot_count NOT BETWEEN 1 AND 80000
  OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'required'
  OR assembly->'policy' IS DISTINCT FROM frozen->'policy'
  OR jsonb_typeof(assembly->'shards') IS DISTINCT FROM 'array'
  OR jsonb_array_length(assembly->'shards')<>(slot_count+499)/500
  OR assembly-'shards' IS DISTINCT FROM jsonb_build_object('schema_version',1,
   'snapshot_digest',frozen->>'snapshot_digest','item_count',item_count,
   'slot_count',slot_count,'policy',frozen->'policy','presets',presets)
  OR EXISTS(SELECT 1 FROM jsonb_array_elements(assembly->'shards') x
   GROUP BY x->>'artifact_id' HAVING count(*)<>1) THEN RETURN false;END IF;
 document_bytes:=octet_length(source->>'assembly_jcs')+
  octet_length(source->>'closure_jcs')+octet_length(source->>'envelope_jcs');
 FOR ref IN SELECT value FROM jsonb_array_elements(assembly->'shards') LOOP
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR ref-'artifact_id'-'sha256'<>'{}'::jsonb
   THEN RETURN false;END IF;
  raw:=photo.v30_delivery_signed_ready_document(p_tenant,p_run,p_task,
   'v30_delivery_slots_'||lpad(shard_index::text,3,'0'),ref);
  IF raw IS NULL OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.artifact_id=(ref->>'artifact_id')::uuid
   AND i.sha256=ref->>'sha256' AND i.contract='DeliverySlotShard') THEN RETURN false;END IF;
  document_bytes:=document_bytes+octet_length(raw);shard:=raw::jsonb;
  IF shard-'items' IS DISTINCT FROM jsonb_build_object('schema_version',1,
   'snapshot_digest',frozen->>'snapshot_digest')
   OR jsonb_typeof(shard->'items') IS DISTINCT FROM 'array'
   OR jsonb_array_length(shard->'items')<>least(500,slot_count-slot_index) THEN RETURN false;END IF;
  FOR item IN SELECT value FROM jsonb_array_elements(shard->'items') LOOP
   ordinal:=slot_index/preset_count;original:=snapshot->'items'->ordinal;
   preset:=presets->(slot_index%preset_count);
   SELECT value INTO cfg FROM jsonb_array_elements(proof->'configs_jcs')
    WHERE value->>'preset_id'=preset->>'preset_id';
   IF cfg IS NULL THEN RETURN false;END IF;
   child:=photo.v30_uuid5(p_run,'child:export:'||slot_index::text);
   request:=jsonb_build_object('schema_version',1,'parent_run_id',p_run,
    'child_run_id',child,'slot_index',slot_index,'ordinal',ordinal,
    'binding',original->'binding','recipe',original->'recipe',
    'snapshot_digest',frozen->>'snapshot_digest','preset',preset,
    'config_jcs',cfg->>'config_jcs');
   observation:=photo.v30_delivery_current_export_observation(p_tenant,p_run,package,request);
   IF observation IS NULL THEN RETURN false;END IF;
   expected:=jsonb_build_object('ordinal',ordinal,'binding',original->'binding',
    'preset_id',preset->>'preset_id','preset_sha',preset->>'preset_sha',
    'required',(preset->>'required')::boolean);
   IF observation->>'run_state'='completed' AND observation->>'export_state'='ready'
    AND (observation->>'artifact_ready')::boolean THEN
    expected:=expected||jsonb_build_object('state','ready',
     'export_id',observation->>'export_id','export',observation->'artifact','reason',NULL);
    IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
     AND i.task_id=p_task AND i.artifact_id=(observation->'artifact'->>'artifact_id')::uuid
     AND i.sha256=observation->'artifact'->>'sha256' AND i.contract='EncodedExport')
     THEN RETURN false;END IF;
   ELSE
    IF observation->>'run_state' NOT IN('completed','completed_with_errors','failed','cancelled')
     OR (preset->>'required')::boolean OR
     (frozen->'policy'->>'allow_omitted')::boolean IS DISTINCT FROM true THEN RETURN false;END IF;
    reason:=CASE WHEN observation->>'run_state'='cancelled' THEN 'EXPORT_CANCELLED'
     WHEN observation->>'run_state'='completed' THEN 'EXPORT_NOT_READY'
     ELSE 'EXPORT_FAILED' END;
    expected:=expected||jsonb_build_object('state','omitted','export_id',NULL,
     'export',NULL,'reason',reason);
   END IF;
   IF item IS DISTINCT FROM expected THEN RETURN false;END IF;
   slot_index:=slot_index+1;
  END LOOP;
  shard_index:=shard_index+1;
 END LOOP;
 IF slot_index<>slot_count OR EXISTS(SELECT 1 FROM photo.v2_task_inputs original
  WHERE original.tenant_id=p_tenant AND original.task_id=(source->>'parent_task_id')::uuid
  AND original.contract NOT IN('Params','InputClosureChunk','InputClosureManifest')
  AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.artifact_id=original.artifact_id AND i.sha256=original.sha256
   AND i.contract=original.contract)) THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
  AND i.artifact_id<>p_params AND NOT EXISTS(SELECT 1 FROM photo.v2_artifact_links l
   WHERE l.tenant_id=p_tenant AND l.workflow_id=workflow AND l.artifact_id=i.artifact_id
    AND l.sha256=i.sha256)) THEN RETURN false;END IF;
 SELECT count(*) INTO input_count FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task;
 IF photo.v30_delivery_assembly_resources_ready((SELECT execution_profile FROM photo.tasks
  WHERE tenant_id=p_tenant AND id=p_task),document_bytes,input_count) IS DISTINCT FROM true
  THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signed_current_matrix(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signed_current_matrix(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
