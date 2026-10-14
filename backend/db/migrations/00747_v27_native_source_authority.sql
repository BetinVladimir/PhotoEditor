-- +goose Up
-- Exact source file cross-binding and retained authority locks. No graph activation.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_file_closure_matches(p_binding jsonb,p_source jsonb,p_params jsonb,p_params_pin jsonb,p_files jsonb,p_registration jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE file jsonb;pin jsonb;expected_object jsonb;actual_rows jsonb;expected_rows jsonb;matched integer:=0;
BEGIN
 IF jsonb_typeof(p_binding) IS DISTINCT FROM 'object' OR jsonb_typeof(p_source) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_source) NOT BETWEEN 2 AND 4096 OR octet_length(p_source::text)>4194304
 OR jsonb_typeof(p_files) IS DISTINCT FROM 'array' OR jsonb_array_length(p_files) NOT BETWEEN 3 AND 4097
 OR octet_length(p_files::text)>4194304 OR jsonb_typeof(p_params) IS DISTINCT FROM 'object'
 OR (p_params-ARRAY['artifact_id','sha256'])<>'{}' OR jsonb_typeof(p_params_pin) IS DISTINCT FROM 'object'
 OR p_registration->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_registration->'params' IS DISTINCT FROM p_params
 OR jsonb_typeof(p_registration->'transport'->'rows') IS DISTINCT FROM 'array'
 OR p_source->0->>'contract' IS DISTINCT FROM 'WorkingImage'
 OR p_source->1->>'contract' IS DISTINCT FROM 'WorkingPixels'
 OR p_source->0->>'artifact_id' IS DISTINCT FROM p_binding->'source'->>'artifact_id'
 OR p_source->0->>'sha256' IS DISTINCT FROM p_binding->'source'->>'sha256' THEN RETURN false;END IF;
 IF (SELECT count(DISTINCT value->'Ref'->>'artifact_id') FROM jsonb_array_elements(p_files))<>jsonb_array_length(p_files)
 OR (SELECT count(*) FROM jsonb_array_elements(p_files) WHERE value->>'Contract'='Params')<>1
 OR (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(p_source))+1<>jsonb_array_length(p_files) THEN RETURN false;END IF;
 FOR pin IN SELECT value FROM jsonb_array_elements(p_source) LOOP
  IF pin->>'contract' IS NULL OR pin->>'contract' NOT IN ('WorkingImage','WorkingPixels','WorkingAlpha','DenseWarpField')
  OR pin->>'artifact_id'=p_params->>'artifact_id' THEN RETURN false;END IF;
  IF pin->>'contract'='WorkingImage' THEN
   IF pin->>'parent_id' IS NOT NULL OR pin->>'parent_sha' IS NOT NULL THEN RETURN false;END IF;
  ELSE
   IF pin->>'parent_id' IS DISTINCT FROM p_binding->'source'->>'artifact_id'
   OR pin->>'parent_sha' IS DISTINCT FROM p_binding->'source'->>'sha256' THEN RETURN false;END IF;
  END IF;
  expected_object:=jsonb_build_object('Key',pin->'key','VersionID',pin->'version','SHA256',pin->'sha256','Bytes',pin->'bytes','ContentType',pin->'media_type');
  IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_files) f WHERE f->>'Contract'=pin->>'contract'
   AND f->'Ref'=jsonb_build_object('artifact_id',pin->'artifact_id','sha256',pin->'sha256') AND f->'Object'=expected_object) THEN RETURN false;END IF;
 END LOOP;
 FOR file IN SELECT value FROM jsonb_array_elements(p_files) LOOP
  IF jsonb_typeof(file) IS DISTINCT FROM 'object' OR (file-ARRAY['Ref','Contract','Object'])<>'{}' THEN RETURN false;END IF;
  IF file->>'Contract'='Params' THEN
   expected_object:=jsonb_build_object('Key',p_params_pin->'key','VersionID',p_params_pin->'version','SHA256',p_params_pin->'sha256','Bytes',p_params_pin->'bytes','ContentType',p_params_pin->'media_type');
   IF file->'Ref' IS DISTINCT FROM p_params OR file->'Object' IS DISTINCT FROM expected_object THEN RETURN false;END IF;
   matched:=matched+1;
  ELSIF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_source) s WHERE s->>'contract'=file->>'Contract'
   AND file->'Ref'=jsonb_build_object('artifact_id',s->'artifact_id','sha256',s->'sha256')) THEN RETURN false;END IF;
 END LOOP;
 SELECT jsonb_agg(jsonb_build_object('ref',value->'Ref','contract',CASE WHEN value->>'Contract' IN ('WorkingPixels','WorkingAlpha','DenseWarpField') THEN 'NpyArray' ELSE value->>'Contract' END) ORDER BY value->'Ref'->>'artifact_id') INTO expected_rows FROM jsonb_array_elements(p_files);
 SELECT jsonb_agg(value ORDER BY value->'ref'->>'artifact_id') INTO actual_rows FROM jsonb_array_elements(p_registration->'transport'->'rows');
 RETURN matched=1 AND actual_rows=expected_rows;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_native_file_closure_matches(jsonb,jsonb,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_file_closure_matches(jsonb,jsonb,jsonb,jsonb,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_file_closure_matches(jsonb,jsonb,jsonb,jsonb,jsonb,jsonb) TO photo_scheduler;

CREATE FUNCTION photo.lock_semantic_native_source_authority(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE binding jsonb;expected jsonb;operation text;qualified boolean;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR jsonb_typeof(p_evidence) IS DISTINCT FROM 'object' OR octet_length(p_evidence::text)>33554432
 OR p_evidence->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_evidence->>'tenant_id' IS DISTINCT FROM p_tenant::text OR p_evidence->>'project_id' IS DISTINCT FROM p_project::text
 OR p_evidence->>'actor_id' IS DISTINCT FROM p_actor::text OR p_evidence->>'session_id' IS DISTINCT FROM p_session::text
 OR p_evidence->'unit_rgb' IS DISTINCT FROM 'true'::jsonb OR coalesce(p_evidence->>'icc_sha','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_evidence->'policy_text') IS DISTINCT FROM 'string' OR octet_length(p_evidence->>'policy_text') NOT BETWEEN 2 AND 4194304 THEN RETURN false;END IF;
 operation:=p_evidence->>'operation';binding:=p_evidence->'binding';expected:=p_evidence->'expected';
 IF operation IS NULL OR operation NOT IN ('face.detect','mask.person')
 OR expected->>'Operation' IS DISTINCT FROM operation OR expected->>'Version' IS DISTINCT FROM '1' OR expected->>'Backend' IS DISTINCT FROM 'cuda'
 OR photo.semantic_native_file_closure_matches(binding,p_evidence->'source_pins',p_evidence->'registration'->'params',p_evidence->'params_pin',p_evidence->'files',p_evidence->'registration') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_proposal_qualification_policy_snapshot(p_tenant,p_project,p_actor,p_session,(p_evidence->>'policy_text')::jsonb) IS NOT TRUE THEN RETURN false;END IF;
 IF operation='face.detect' THEN
  qualified:=photo.lock_semantic_proposal_face_qualification(p_tenant,p_project,p_actor,p_session,expected,p_evidence->'qualification');
 ELSE
  qualified:=photo.lock_semantic_proposal_person_qualification(p_tenant,p_project,p_actor,p_session,expected,p_evidence->'qualification');
 END IF;
 IF qualified IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_proposal_source_evidence(p_tenant,p_project,p_actor,p_session,(binding->>'asset_version_id')::uuid,(binding->'source'->>'artifact_id')::uuid,binding->'source'->>'sha256',p_evidence->'source_pins') IS NOT TRUE THEN RETURN false;END IF;
 RETURN photo.lock_semantic_defect_version_params(p_tenant,p_project,p_actor,p_session,(binding->>'asset_version_id')::uuid,(binding->'source'->>'artifact_id')::uuid,binding->'source'->>'sha256',(p_evidence->>'params_document_id')::uuid,(p_evidence->'registration'->'params'->>'artifact_id')::uuid,p_evidence->'registration'->'params'->>'sha256',p_evidence->'params_pin');
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_source_authority(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_source_authority(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_source_authority(uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
