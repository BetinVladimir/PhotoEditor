-- +goose Up
-- Evidence-bound output sidecars, never arbitrary tenant artifact lookup.
-- Both sidecars must originate from the canonical evidence task attempt/fence.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preapply_preview_output_sources(p_tenant uuid,p_preview uuid,
 p_actor uuid,p_session uuid,p_body text,p_sha text,p_evidence_body text)
RETURNS TABLE(role text,artifact_id uuid,sha256 text,bucket text,object_key text,
 object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE evidence record;e jsonb;item record;a photo.artifacts%ROWTYPE;
BEGIN
 SELECT * INTO evidence FROM photo.semantic_preapply_preview_evidence_source(p_tenant,p_preview,p_actor,p_session,p_body,p_sha);
 IF evidence.artifact_id IS NULL THEN RETURN;END IF;
 IF p_evidence_body IS NULL OR octet_length(p_evidence_body) NOT BETWEEN 2 AND 1048576
 OR octet_length(p_evidence_body) IS DISTINCT FROM evidence.bytes
 OR encode(public.digest(convert_to(p_evidence_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM evidence.sha256 THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_EVIDENCE_INVALID' USING ERRCODE='22023';END IF;
 e:=p_evidence_body::jsonb;
 IF jsonb_typeof(e) IS DISTINCT FROM 'object' OR e->'params' IS DISTINCT FROM p_body::jsonb
 OR e->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR e->>'algorithm' IS DISTINCT FROM 'local_exposure_exact_replay_v1'
 OR e->'protected_pixels_unchanged' IS DISTINCT FROM 'true'::jsonb
 OR e->'outside_mask_max_error' IS DISTINCT FROM '0'::jsonb
 OR jsonb_typeof(e->'working_image') IS DISTINCT FROM 'object'
 OR jsonb_typeof(e->'pixels') IS DISTINCT FROM 'object'
 OR e->'working_image'->>'artifact_id' IS NOT DISTINCT FROM e->'pixels'->>'artifact_id' THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_EVIDENCE_INVALID' USING ERRCODE='22023';END IF;
 FOR item IN SELECT * FROM (VALUES('working_image',e->'working_image','application/json',1048576::bigint),
 ('pixels',e->'pixels','application/x-npy',2400016384::bigint)) AS outputs(role,ref,media,maximum) LOOP
 SELECT * INTO a FROM photo.artifacts object WHERE object.tenant_id=p_tenant
 AND object.id=(item.ref->>'artifact_id')::uuid AND object.sha256=item.ref->>'sha256';
 IF a.id IS NULL OR a.id=evidence.artifact_id OR a.created_by_task_id IS DISTINCT FROM evidence.task_id
 OR a.produced_attempt IS DISTINCT FROM evidence.attempt OR a.produced_fence IS DISTINCT FROM evidence.fence
 OR a.output_role IS DISTINCT FROM item.role OR a.media_type IS DISTINCT FROM item.media
 OR a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL
 OR a.bytes IS NULL OR a.bytes NOT BETWEEN 2 AND item.maximum
 OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_OUTPUT_UNAVAILABLE' USING ERRCODE='42501';END IF;
 role:=item.role;artifact_id:=a.id;sha256:=a.sha256;bucket:=a.bucket;object_key:=a.object_key;
 object_version:=a.object_version;bytes:=a.bytes;media_type:=a.media_type;RETURN NEXT;
 END LOOP;
END $$;
ALTER FUNCTION photo.semantic_preapply_preview_output_sources(uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preapply_preview_output_sources(uuid,uuid,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_preapply_preview_output_sources(uuid,uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
