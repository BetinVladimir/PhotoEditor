-- +goose Up
-- Downstream metadata matrix support; admission gates remain separate.
-- +goose StatementBegin

DO $$ BEGIN IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='photo.v30_delivery_shard_scope(uuid,uuid,text,text)'::regprocedure) IS DISTINCT FROM 'ec5bb5297061984182c4f3197df3e911' THEN RAISE EXCEPTION 'DELIVERY_MATRIX_DEFINITION_DRIFT';END IF;END $$;

CREATE OR REPLACE FUNCTION photo.v30_delivery_shard_scope(p_expansion uuid,p_token uuid,p_purpose text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE payload jsonb;e photo.graph_expansions%ROWTYPE;frozen jsonb;body jsonb;request jsonb;observation jsonb;
 expected jsonb;item jsonb;shard integer;first_slot integer;count_slots integer;idx integer;reason text;
BEGIN
 IF p_purpose IS NULL OR p_purpose !~ '^v30_delivery_slots_[0-9]{3}$'
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 shard:=right(p_purpose,3)::integer;IF shard>=160 THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=e.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=e.tenant_id FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 payload:=photo.v30_delivery_expansion_plan(p_expansion,p_token);
 IF payload IS NULL OR e.cursor::integer<>e.expected_count THEN RETURN NULL;END IF;
 frozen:=(payload->'proof'->>'frozen_input_jcs')::jsonb;body:=p_body::jsonb;
 IF (frozen->'policy'->>'metadata_policy' IS NULL OR frozen->'policy'->>'metadata_policy' NOT IN('strip','copyright','approved_exif'))
 OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'off' THEN RETURN NULL;END IF;
 first_slot:=shard*500;count_slots:=least(500,e.expected_count-first_slot);
 IF count_slots<1 OR jsonb_typeof(body->'items') IS DISTINCT FROM 'array'
 OR jsonb_array_length(body->'items')<>count_slots
 OR body-'items' IS DISTINCT FROM jsonb_build_object('schema_version',1,'snapshot_digest',frozen->>'snapshot_digest') THEN RETURN NULL;END IF;
 FOR idx IN 0..count_slots-1 LOOP
  request:=photo.v30_delivery_child_request(p_expansion,p_token,first_slot+idx);
  observation:=photo.v30_delivery_export_observation(p_expansion,p_token,first_slot+idx);
  IF request IS NULL OR observation IS NULL THEN RETURN NULL;END IF;
  expected:=jsonb_build_object('ordinal',(request->>'ordinal')::integer,'binding',request->'binding',
   'preset_id',request->'preset'->>'preset_id','preset_sha',request->'preset'->>'preset_sha',
   'required',(request->'preset'->>'required')::boolean);
  IF observation->>'run_state'='completed' AND observation->>'export_state'='ready' AND (observation->>'artifact_ready')::boolean THEN
   expected:=expected||jsonb_build_object('state','ready','export_id',observation->>'export_id','export',observation->'artifact','reason',NULL);
  ELSE
   IF observation->>'run_state' NOT IN('completed','completed_with_errors','failed','cancelled')
   OR (request->'preset'->>'required')::boolean OR (frozen->'policy'->>'allow_omitted')::boolean IS DISTINCT FROM true THEN RETURN NULL;END IF;
   reason:=CASE WHEN observation->>'run_state'='cancelled' THEN 'EXPORT_CANCELLED'
    WHEN observation->>'run_state'='completed' THEN 'EXPORT_NOT_READY' ELSE 'EXPORT_FAILED' END;
   expected:=expected||jsonb_build_object('state','omitted','export_id',NULL,'export',NULL,'reason',reason);
  END IF;
  item:=body->'items'->idx;IF item IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 END LOOP;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=p_expansion AND expansion_token=p_token AND expansion_lease_until>clock_timestamp()) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('tenant',e.tenant_id,'run',e.run_id,
 'owner',photo.v30_uuid5(photo.v30_uuid5(e.run_id,'workflow'),'v2.delivery.assemble:0'),
 'sha',encode(sha256(convert_to(p_body,'UTF8')),'hex'),'bytes',octet_length(p_body));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;

ALTER FUNCTION photo.v30_delivery_shard_scope(uuid,uuid,text,text) OWNER TO photo_fn_owner;

DO $$ BEGIN IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='photo.v30_delivery_assembly_source_ready(uuid,uuid,uuid,uuid,text,text)'::regprocedure) IS DISTINCT FROM '186d986065bc4b1c8a952802aaae676d' THEN RAISE EXCEPTION 'DELIVERY_MATRIX_DEFINITION_DRIFT';END IF;END $$;

CREATE OR REPLACE FUNCTION photo.v30_delivery_assembly_source_ready(p_tenant uuid,p_run uuid,p_task uuid,p_params uuid,p_sha text,p_json text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE context jsonb;proof jsonb;frozen jsonb;snapshot jsonb;body jsonb;presets jsonb;ref jsonb;
 shard jsonb;preset jsonb;cfg jsonb;source jsonb;request jsonb;observation jsonb;expected jsonb;item jsonb;
 d photo.backend_documents%ROWTYPE;workflow uuid;package_id uuid;child uuid;reason text;
 count_presets integer;count_items integer;slots integer;index_shard integer:=0;index_slot integer:=0;ordinal integer;document_bytes bigint;inputs_count bigint;
BEGIN
 context:=photo.v30_delivery_assembly_source_context(p_tenant,p_run,p_task,p_params,p_sha,p_json);
 IF context IS NULL OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 workflow:=photo.v30_uuid5(p_run,'workflow');package_id:=(context->>'package_id')::uuid;
 proof:=context->'proof';frozen:=(proof->>'frozen_input_jcs')::jsonb;snapshot:=(proof->>'snapshot_jcs')::jsonb;body:=(context->>'assembly_jcs')::jsonb;
 SELECT jsonb_agg(value ORDER BY value->>'preset_id') INTO presets FROM jsonb_array_elements(frozen->'presets');
 count_presets:=jsonb_array_length(presets);count_items:=jsonb_array_length(snapshot->'items');slots:=count_presets*count_items;
 IF count_presets NOT BETWEEN 1 AND 16 OR count_items NOT BETWEEN 1 AND 5000 OR slots NOT BETWEEN 1 AND 80000
 OR (frozen->'policy'->>'metadata_policy' IS NULL OR frozen->'policy'->>'metadata_policy' NOT IN('strip','copyright','approved_exif')) OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'off'
 OR jsonb_typeof(body->'shards') IS DISTINCT FROM 'array' OR jsonb_array_length(body->'shards')<>(slots+499)/500
 OR body-'shards' IS DISTINCT FROM jsonb_build_object('schema_version',1,'snapshot_digest',frozen->>'snapshot_digest',
 'item_count',count_items,'slot_count',slots,'policy',frozen->'policy','presets',presets) THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(body->'shards') x GROUP BY x->>'artifact_id' HAVING count(*)<>1) THEN RETURN false;END IF;
 document_bytes:=octet_length(context->>'assembly_jcs');
 FOR ref IN SELECT value FROM jsonb_array_elements(body->'shards') LOOP
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR ref-'artifact_id'-'sha256'<>'{}'::jsonb THEN RETURN false;END IF;
  SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
  WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_task AND b.purpose='v30_delivery_slots_'||lpad(index_shard::text,3,'0')
  AND b.artifact_id=(ref->>'artifact_id')::uuid AND b.sha256=ref->>'sha256' AND b.state='ready' AND b.v30_delivery_document_jcs IS NOT NULL
  AND a.state='ready' AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket AND a.object_version=b.object_version
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND a.verified_at IS NOT NULL
  AND a.media_type='application/json' AND a.output_role=b.purpose
  AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||b.artifact_id
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
  IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task
  AND artifact_id=d.artifact_id AND sha256=d.sha256 AND contract='DeliverySlotShard') THEN RETURN false;END IF;
  shard:=d.v30_delivery_document_jcs::jsonb;document_bytes:=document_bytes+d.bytes;
  IF shard-'items' IS DISTINCT FROM jsonb_build_object('schema_version',1,'snapshot_digest',frozen->>'snapshot_digest')
  OR jsonb_typeof(shard->'items') IS DISTINCT FROM 'array' OR jsonb_array_length(shard->'items')<>least(500,slots-index_slot) THEN RETURN false;END IF;
  FOR item IN SELECT value FROM jsonb_array_elements(shard->'items') LOOP
   ordinal:=index_slot/count_presets;source:=snapshot->'items'->ordinal;preset:=presets->(index_slot%count_presets);
   SELECT value INTO cfg FROM jsonb_array_elements(proof->'configs_jcs') WHERE value->>'preset_id'=preset->>'preset_id';
   IF cfg IS NULL THEN RETURN false;END IF;
   child:=photo.v30_uuid5(p_run,'child:export:'||index_slot::text);
   request:=jsonb_build_object('schema_version',1,'parent_run_id',p_run,'child_run_id',child,'slot_index',index_slot,'ordinal',ordinal,
   'binding',source->'binding','recipe',source->'recipe','snapshot_digest',frozen->>'snapshot_digest','preset',preset,'config_jcs',cfg->>'config_jcs');
   observation:=photo.v30_delivery_current_export_observation(p_tenant,p_run,package_id,request);
   IF observation IS NULL THEN RETURN false;END IF;
   expected:=jsonb_build_object('ordinal',ordinal,'binding',source->'binding','preset_id',preset->>'preset_id',
   'preset_sha',preset->>'preset_sha','required',(preset->>'required')::boolean);
   IF observation->>'run_state'='completed' AND observation->>'export_state'='ready' AND (observation->>'artifact_ready')::boolean THEN
    expected:=expected||jsonb_build_object('state','ready','export_id',observation->>'export_id','export',observation->'artifact','reason',NULL);
    IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task
    AND artifact_id=(observation->'artifact'->>'artifact_id')::uuid AND sha256=observation->'artifact'->>'sha256') THEN RETURN false;END IF;
   ELSE
    IF observation->>'run_state' NOT IN('completed','completed_with_errors','failed','cancelled')
    OR (preset->>'required')::boolean OR (frozen->'policy'->>'allow_omitted')::boolean IS DISTINCT FROM true THEN RETURN false;END IF;
    reason:=CASE WHEN observation->>'run_state'='cancelled' THEN 'EXPORT_CANCELLED' WHEN observation->>'run_state'='completed' THEN 'EXPORT_NOT_READY' ELSE 'EXPORT_FAILED' END;
    expected:=expected||jsonb_build_object('state','omitted','export_id',NULL,'export',NULL,'reason',reason);
   END IF;
   IF item IS DISTINCT FROM expected THEN RETURN false;END IF;
   index_slot:=index_slot+1;
  END LOOP;
  index_shard:=index_shard+1;
 END LOOP;
 IF index_slot<>slots OR EXISTS(SELECT 1 FROM photo.v2_task_inputs original WHERE original.tenant_id=p_tenant
 AND original.task_id=(context->>'parent_task_id')::uuid AND original.contract NOT IN('Params','InputClosureChunk','InputClosureManifest')
 AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
 AND i.artifact_id=original.artifact_id AND i.sha256=original.sha256 AND i.contract=original.contract)) THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id<>p_params
 AND NOT EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=p_tenant AND l.workflow_id=workflow AND l.artifact_id=i.artifact_id AND l.sha256=i.sha256)) THEN RETURN false;END IF;
 SELECT count(*) INTO inputs_count FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task;
 IF photo.v30_delivery_assembly_resources_ready((SELECT execution_profile FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task),document_bytes,inputs_count) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;

ALTER FUNCTION photo.v30_delivery_assembly_source_ready(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
