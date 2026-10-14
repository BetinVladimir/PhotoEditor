-- +goose Up
-- Compose original native admission with complete independently locked manual evidence.
-- No graph activation or receipt creation.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_manual_admission_evidence(p_tenant uuid,p_run uuid,p_packet jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE native_packet jsonb;face jsonb;config jsonb;params jsonb;manual jsonb;pin jsonb;target jsonb;protected_pin jsonb;actor uuid;session_id uuid;project uuid;binding jsonb;phase integer;sealed boolean;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_packet) IS DISTINCT FROM 'object' OR octet_length(p_packet::text)>33554432
 OR p_packet->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_packet-ARRAY['schema_version','native','manual_evidence','params_text'])<>'{}'
 OR jsonb_typeof(p_packet->'native') IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_packet->'manual_evidence') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_packet->'params_text') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 IF jsonb_array_length(p_packet->'manual_evidence') NOT BETWEEN 1 AND 64 OR octet_length(p_packet->>'params_text') NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 native_packet:=p_packet->'native';face:=native_packet->'landmarks'->'admission'->'face';binding:=face->'binding';
 actor:=(face->>'actor_id')::uuid;session_id:=(face->>'session_id')::uuid;project:=(face->>'project_id')::uuid;
 IF photo.v2_editor_authorized(p_tenant,project,actor,session_id) IS NOT TRUE THEN RETURN false;END IF;
 config:=(native_packet->>'config_text')::jsonb->'v27_defect_native_prepare';
 params:=(p_packet->>'params_text')::jsonb;
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' OR (params-ARRAY['image','faces','permanent','protections','diameter_face_fraction'])<>'{}'
 OR params->'image' IS DISTINCT FROM binding->'source'
 OR encode(public.digest(convert_to(p_packet->>'params_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM config->'params'->>'sha256'
 OR octet_length(p_packet->>'params_text') IS DISTINCT FROM (native_packet->'params_pin'->>'bytes')::bigint
 OR jsonb_typeof(params->'faces') IS DISTINCT FROM 'array' OR jsonb_array_length(params->'faces')>64
 OR jsonb_typeof(coalesce(params->'protections','[]'::jsonb)) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 -- Each target is selected by original Params; no omitted or injected manual producer.
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(p_packet->'manual_evidence') e WHERE jsonb_typeof(e) IS DISTINCT FROM 'array') THEN RETURN false;END IF;
 IF (SELECT count(DISTINCT pin->>'artifact_id') FROM jsonb_array_elements(p_packet->'manual_evidence') e CROSS JOIN LATERAL jsonb_array_elements(e) pin WHERE pin->>'role'='rasterized_mask')<>jsonb_array_length(p_packet->'manual_evidence') THEN RETURN false;END IF;
 FOR manual IN SELECT value FROM jsonb_array_elements(p_packet->'manual_evidence') LOOP
  SELECT value INTO target FROM jsonb_array_elements(manual) WHERE value->>'role'='mask_target';
  SELECT value INTO protected_pin FROM jsonb_array_elements(manual) WHERE value->>'role'='mask_protected';
  IF target IS NULL OR protected_pin IS NULL OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(params->'faces') f WHERE f->'manual'->'artifact'=jsonb_build_object('artifact_id',target->>'artifact_id','sha256',target->>'sha256')) THEN RETURN false;END IF;
  IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(coalesce(params->'protections','[]'::jsonb)) m WHERE m->'artifact'=jsonb_build_object('artifact_id',protected_pin->>'artifact_id','sha256',protected_pin->>'sha256'))
   AND params->'permanent'->'artifact' IS DISTINCT FROM jsonb_build_object('artifact_id',protected_pin->>'artifact_id','sha256',protected_pin->>'sha256') THEN RETURN false;END IF;
  FOR pin IN SELECT value FROM jsonb_array_elements(manual) WHERE value->>'role' IN('rasterized_mask','mask_target','mask_protected') LOOP
   IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(config->'input_entries') entry WHERE entry->'ref'->>'artifact_id'=pin->>'artifact_id' AND entry->'ref'->>'sha256'=pin->>'sha256') THEN RETURN false;END IF;
  END LOOP;
 END LOOP;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(params->'faces') f WHERE f->'manual' IS NOT NULL AND f->'manual'<>'null'::jsonb AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_packet->'manual_evidence') e CROSS JOIN LATERAL jsonb_array_elements(e) pin WHERE pin->>'role'='mask_target' AND f->'manual'->'artifact'=jsonb_build_object('artifact_id',pin->>'artifact_id','sha256',pin->>'sha256'))) THEN RETURN false;END IF;
 -- Independent permanent/additional protected authorities are not implemented by this gate.
 IF EXISTS(SELECT 1 FROM (SELECT value AS mask FROM jsonb_array_elements(coalesce(params->'protections','[]'::jsonb)) UNION ALL SELECT params->'permanent' WHERE params->'permanent' IS NOT NULL AND params->'permanent'<>'null'::jsonb) masks WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_packet->'manual_evidence') e CROSS JOIN LATERAL jsonb_array_elements(e) pin WHERE pin->>'role'='mask_protected' AND masks.mask->'artifact'=jsonb_build_object('artifact_id',pin->>'artifact_id','sha256',pin->>'sha256'))) THEN RETURN false;END IF;
 -- Match the native atomic seal lock order: tenant, usage, then run.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT graph_sealed INTO sealed FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF sealed IS NULL THEN RETURN false;END IF;
 IF sealed THEN
  IF photo.lock_semantic_native_defect_completed_evidence(p_tenant,p_run,native_packet) IS NOT TRUE THEN RETURN false;END IF;
 ELSE
  IF photo.lock_semantic_native_defect_admission_evidence(p_tenant,p_run,native_packet) IS NOT TRUE THEN RETURN false;END IF;
 END IF;
 FOR phase IN 1..2 LOOP
  FOR manual IN SELECT roots.evidence FROM jsonb_array_elements(p_packet->'manual_evidence') AS roots(evidence) ORDER BY (SELECT pin->>'artifact_id' FROM jsonb_array_elements(roots.evidence) pin WHERE pin->>'role'='rasterized_mask') LOOP
   SELECT value INTO target FROM jsonb_array_elements(manual) WHERE value->>'role'='mask_target';
   IF photo.lock_semantic_proposal_manual_evidence(p_tenant,project,actor,session_id,(binding->>'asset_version_id')::uuid,(binding->'source'->>'artifact_id')::uuid,binding->'source'->>'sha256',(target->>'artifact_id')::uuid,target->>'sha256',manual) IS NOT TRUE THEN RETURN false;END IF;
  END LOOP;
  IF phase=1 AND photo.lock_semantic_proposal_person_inventory(p_tenant,project,actor,session_id) IS NOT TRUE THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range OR cardinality_violation THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_manual_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_manual_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_manual_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
