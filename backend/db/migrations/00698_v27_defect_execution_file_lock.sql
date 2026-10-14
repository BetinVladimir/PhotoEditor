-- +goose Up
-- Exact declared-file metadata for an existing canonical defect origin only.
-- Producer/policy/inventory/auth barriers and queued admission remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_defect_execution_files(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_origin jsonb,p_files jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb; a photo.artifacts%ROWTYPE; actual jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR jsonb_typeof(p_origin) IS DISTINCT FROM 'array' OR jsonb_array_length(p_origin)<>5
 OR jsonb_typeof(p_files) IS DISTINCT FROM 'array' OR pg_column_size(p_files)>4194304 THEN RETURN false; END IF;
 IF jsonb_array_length(p_files)<2 OR jsonb_array_length(p_files)>4097 THEN RETURN false; END IF;
 IF photo.semantic_proposal_defect_origin(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,
 (p_origin->0->>'artifact_id')::uuid,p_origin->0->>'sha256',
 (p_origin->1->>'artifact_id')::uuid,p_origin->1->>'sha256',
 (p_origin->2->>'artifact_id')::uuid,p_origin->2->>'sha256',
 (p_origin->3->>'artifact_id')::uuid,p_origin->3->>'sha256',
 (p_origin->4->>'artifact_id')::uuid,p_origin->4->>'sha256') IS NOT TRUE THEN RETURN false; END IF;
 IF (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(p_files))<>jsonb_array_length(p_files)
 OR (SELECT count(*) FROM jsonb_array_elements(p_files) WHERE value->>'contract'='Params')<>1 THEN RETURN false; END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant
 AND id IN(SELECT (value->>'artifact_id')::uuid FROM jsonb_array_elements(p_files)) ORDER BY id FOR SHARE;
 FOR pin IN SELECT value FROM jsonb_array_elements(p_files) LOOP
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->>'artifact_id')::uuid;
  IF NOT FOUND OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL
  OR a.object_version IN('','null') OR a.bytes<=0 OR a.expires_at<=clock_timestamp() THEN RETURN false; END IF;
  IF pin->>'contract'='Params' THEN
   IF a.id IS DISTINCT FROM (p_origin->4->>'artifact_id')::uuid OR a.sha256 IS DISTINCT FROM p_origin->4->>'sha256'
   OR pin->>'parent_id' IS NOT NULL OR pin->>'parent_sha' IS NOT NULL THEN RETURN false; END IF;
  ELSIF pin->>'contract'='FaceLandmarks' THEN
   IF pin->>'parent_id' IS NOT NULL OR pin->>'parent_sha' IS NOT NULL THEN RETURN false; END IF;
   PERFORM 1 FROM photo.semantic_proposal_face_preparation_object(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,a.id,a.sha256,'face.landmarks');
   IF NOT FOUND THEN RETURN false; END IF;
  ELSIF pin->>'contract' IN('WorkingImage','WorkingPixels','WorkingAlpha','DenseWarpField','MaskRef','MaskPixels') THEN
   IF photo.v2_draft_artifact_scope(p_tenant,p_project,p_version,a.id,a.sha256,pin->>'contract',p_actor,p_session,(pin->>'parent_id')::uuid,pin->>'parent_sha') IS NOT TRUE THEN RETURN false; END IF;
  ELSE RETURN false;
  END IF;
  actual:=jsonb_build_object('contract',pin->>'contract','artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'key',a.object_key,'version',a.object_version,'bytes',a.bytes,'media_type',a.media_type,'parent_id',pin->>'parent_id','parent_sha',pin->>'parent_sha');
  IF actual IS DISTINCT FROM pin THEN RETURN false; END IF;
 END LOOP;
 RETURN true;
END $$;
ALTER FUNCTION photo.lock_semantic_defect_execution_files(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_execution_files(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_execution_files(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
