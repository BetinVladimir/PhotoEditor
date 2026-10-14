-- +goose Up
-- Authenticated WorkingImage bytes select an exact child, never an arbitrary Ref.
-- This exposes metadata only; physical NPY and color/geometry checks remain required.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_image_binary(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_target uuid,p_target_revision bigint,p_version uuid,p_candidate uuid,p_candidate_sha text,p_reference boolean,p_source uuid,p_source_sha text,p_body text,p_kind text,p_step integer)
RETURNS TABLE(contract text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE root record;body jsonb;pin jsonb;
BEGIN
 SELECT * INTO root FROM photo.semantic_approved_image_object(p_tenant,p_intent,p_actor,p_session,p_revision,p_target,p_target_revision,p_version,p_candidate,p_candidate_sha,p_reference,p_source,p_source_sha);
 IF root.artifact_id IS NULL OR p_body IS NULL OR octet_length(p_body)<>root.bytes
 OR encode(sha256(convert_to(p_body,'UTF8')),'hex') IS DISTINCT FROM root.sha256 THEN RETURN;END IF;
 body:=p_body::jsonb;
 IF p_kind IN('pixels','alpha') AND p_step=-1 THEN pin:=body->p_kind;
 ELSIF p_kind='inverse_field' AND p_step BETWEEN 0 AND 31 AND body->'transform_chain'->'steps'->p_step->>'kind'='dense_warp' THEN
 pin:=body->'transform_chain'->'steps'->p_step->'inverse_field';
 ELSE RETURN;END IF;
 IF jsonb_typeof(pin) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(pin))<>2
 OR coalesce(pin->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 RETURN QUERY SELECT 'NpyArray'::text,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(pin->>'artifact_id')::uuid AND a.sha256=pin->>'sha256'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 32 AND 2400016384
 AND a.media_type IN('application/octet-stream','application/x-npy')
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.semantic_approved_image_binary(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,boolean,uuid,text,text,text,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_image_binary(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,boolean,uuid,text,text,text,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_approved_image_binary(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,boolean,uuid,text,text,text,integer) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
