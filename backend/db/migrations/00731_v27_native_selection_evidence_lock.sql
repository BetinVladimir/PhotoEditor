-- +goose Up
-- Exact privately verified selection objects; content/separation verified before TX.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_selection_evidence(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_canvas text,p_root uuid,p_root_sha text,p_snapshot uuid,p_expected jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid; pin jsonb; a photo.artifacts%ROWTYPE; actual jsonb;
BEGIN
 IF jsonb_typeof(p_expected) IS DISTINCT FROM 'array' OR octet_length(p_expected::text)>16384 THEN RETURN false;END IF;
 IF jsonb_array_length(p_expected)<>2 THEN RETURN false;END IF;
 SELECT i.project_id INTO project FROM photo.lock_semantic_proposal_inventory(p_tenant,p_actor,p_session,p_version,p_source,p_source_sha,p_canvas,p_root,p_root_sha,p_snapshot) i LIMIT 1;
 IF project IS DISTINCT FROM p_project OR project IS NULL THEN RETURN false;END IF;
 IF p_expected->0->>'contract' IS DISTINCT FROM 'MaskRef'
 OR p_expected->0->>'artifact_id' IS DISTINCT FROM p_root::text
 OR p_expected->0->>'sha256' IS DISTINCT FROM p_root_sha
 OR p_expected->0->>'parent_id' IS NOT NULL OR p_expected->0->>'parent_sha' IS NOT NULL
 OR p_expected->1->>'contract' IS DISTINCT FROM 'MaskPixels'
 OR p_expected->1->>'parent_id' IS DISTINCT FROM p_root::text
 OR p_expected->1->>'parent_sha' IS DISTINCT FROM p_root_sha
 OR p_expected->1->>'artifact_id' IS NULL OR p_expected->1->>'artifact_id'=p_root::text THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN(SELECT (value->>'artifact_id')::uuid FROM jsonb_array_elements(p_expected)) ORDER BY id FOR SHARE;
 PERFORM document_id FROM photo.v2_draft_uploads WHERE tenant_id=p_tenant AND artifact_id IN(SELECT (value->>'artifact_id')::uuid FROM jsonb_array_elements(p_expected)) ORDER BY document_id FOR SHARE;
 FOR pin IN SELECT value FROM jsonb_array_elements(p_expected) LOOP
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->>'artifact_id')::uuid;
  IF NOT FOUND OR a.state<>'ready' OR a.verified_at IS NULL OR a.bytes<=0 OR a.object_version IS NULL OR a.object_version IN('','null') OR a.expires_at<=clock_timestamp() THEN RETURN false;END IF;
  IF photo.v2_draft_artifact_scope(p_tenant,p_project,p_version,a.id,a.sha256,pin->>'contract',p_actor,p_session,(pin->>'parent_id')::uuid,pin->>'parent_sha') IS NOT TRUE THEN RETURN false;END IF;
  actual:=jsonb_build_object('contract',pin->>'contract','artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'key',a.object_key,'version',a.object_version,'bytes',a.bytes,'media_type',a.media_type,'parent_id',pin->>'parent_id','parent_sha',pin->>'parent_sha');
  IF actual IS DISTINCT FROM pin THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_selection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_selection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_selection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
