-- +goose Up
-- Selected source closure only; complete authority/inventory locks remain caller obligations.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_proposal_source_evidence(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_pins jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb; a photo.artifacts%ROWTYPE; actual jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR jsonb_typeof(p_pins) IS DISTINCT FROM 'array' OR pg_column_size(p_pins)>4194304 THEN RETURN false; END IF;
 IF jsonb_array_length(p_pins)<2 THEN RETURN false; END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN(SELECT (value->>'artifact_id')::uuid FROM jsonb_array_elements(p_pins)) ORDER BY id FOR SHARE;
 FOR pin IN SELECT value FROM jsonb_array_elements(p_pins) LOOP
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->>'artifact_id')::uuid;
  IF NOT FOUND THEN RETURN false; END IF;
  IF pin->>'contract' IS NULL OR pin->>'contract' NOT IN('WorkingImage','WorkingPixels','WorkingAlpha','DenseWarpField') THEN RETURN false; END IF;
  IF pin->>'contract'='WorkingImage' THEN
   IF a.id<>p_source OR a.sha256<>p_source_sha OR pin->>'parent_id' IS NOT NULL OR pin->>'parent_sha' IS NOT NULL THEN RETURN false; END IF;
  ELSE
   IF pin->>'parent_id' IS DISTINCT FROM p_source::text OR pin->>'parent_sha' IS DISTINCT FROM p_source_sha THEN RETURN false; END IF;
  END IF;
  IF photo.v2_draft_artifact_scope(p_tenant,p_project,p_version,a.id,a.sha256,pin->>'contract',p_actor,p_session,(pin->>'parent_id')::uuid,pin->>'parent_sha') IS NOT TRUE THEN RETURN false; END IF;
  IF a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR a.expires_at<=clock_timestamp() THEN RETURN false; END IF;
  actual:=jsonb_build_object('contract',pin->>'contract','artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'key',a.object_key,'version',a.object_version,'bytes',a.bytes,'media_type',a.media_type,'parent_id',pin->>'parent_id','parent_sha',pin->>'parent_sha');
  IF actual IS DISTINCT FROM pin THEN RETURN false; END IF;
 END LOOP;
 RETURN true;
END $$;
ALTER FUNCTION photo.lock_semantic_proposal_source_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_source_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_source_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
