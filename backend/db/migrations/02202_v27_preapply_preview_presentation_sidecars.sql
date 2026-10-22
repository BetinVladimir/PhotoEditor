-- +goose Up
-- Presentation-bound sidecars; raw bytes must match the canonical presentation.
-- All sidecars must originate from the same canonical task attempt/fence.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preapply_preview_presentation_sidecars(p_tenant uuid,p_preview uuid,
 p_actor uuid,p_session uuid,p_body text,p_sha text,p_presentation_body text)
RETURNS TABLE(role text,artifact_id uuid,sha256 text,bucket text,object_key text,
 object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE evidence record;e jsonb;item record;a photo.artifacts%ROWTYPE;
BEGIN
 SELECT * INTO evidence FROM photo.semantic_preapply_preview_presentation_source(p_tenant,p_preview,p_actor,p_session,p_body,p_sha);
 IF evidence.artifact_id IS NULL THEN RETURN;END IF;
 IF p_presentation_body IS NULL OR octet_length(p_presentation_body) NOT BETWEEN 2 AND 1048576
 OR octet_length(p_presentation_body) IS DISTINCT FROM evidence.bytes
 OR encode(public.digest(convert_to(p_presentation_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM evidence.sha256 THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_PRESENTATION_INVALID' USING ERRCODE='22023';END IF;
 e:=p_presentation_body::jsonb;
 IF jsonb_typeof(e) IS DISTINCT FROM 'object' OR e->'params' IS DISTINCT FROM p_body::jsonb
 OR e->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR e->>'resize' IS DISTINCT FROM 'area_linear_premult_v1'
 OR e->>'overlay' IS DISTINCT FROM 'max_coverage_ceil_alpha_v1'
 OR jsonb_typeof(e->'width') IS DISTINCT FROM 'number'
 OR jsonb_typeof(e->'height') IS DISTINCT FROM 'number'
 OR e->>'width' !~ '^[1-9][0-9]{0,3}$' OR e->>'height' !~ '^[1-9][0-9]{0,3}$'
 OR (e->>'width')::integer>2560 OR (e->>'height')::integer>2560
 OR e->>'icc_lock_sha' IS NULL OR e->>'icc_lock_sha' !~ '^[0-9a-f]{64}$'
 OR e->>'srgb_sha' IS NULL OR e->>'srgb_sha' !~ '^[0-9a-f]{64}$'
 OR NOT(e ?& ARRAY['numerical_evidence','working_image','pixels','before','after','affected','protected'])
 OR (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_each(e)
     WHERE key IN('numerical_evidence','working_image','pixels','before','after','affected','protected'))<>7 THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_PRESENTATION_INVALID' USING ERRCODE='22023';END IF;
 FOR item IN SELECT * FROM (VALUES('working_image',e->'working_image','application/json',1048576::bigint),
 ('pixels',e->'pixels','application/x-npy',2400016384::bigint),
 ('preview_evidence',e->'numerical_evidence','application/json',1048576::bigint),
 ('before',e->'before','image/png',33882112::bigint),
 ('after',e->'after','image/png',33882112::bigint),
 ('affected',e->'affected','image/png',32833536::bigint),
 ('protected',e->'protected','image/png',32833536::bigint)) AS outputs(role,ref,media,maximum) LOOP
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
ALTER FUNCTION photo.semantic_preapply_preview_presentation_sidecars(uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preapply_preview_presentation_sidecars(uuid,uuid,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_preapply_preview_presentation_sidecars(uuid,uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
