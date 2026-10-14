-- +goose Up
-- Exact original candidate -> exact mask document -> same-attempt mask pixels.
-- Physical shape/range/hash verification remains mandatory in the reader.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_mask_binary(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_target uuid,p_target_revision bigint,p_version uuid,p_candidate uuid,p_candidate_sha text,p_candidate_body text,p_mask_body text)
RETURNS TABLE(contract text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE root record;body jsonb;pin jsonb;producer uuid;attempt integer;fence bigint;
BEGIN
 SELECT * INTO root FROM photo.semantic_approved_candidate_child(p_tenant,p_intent,p_actor,p_session,p_revision,p_target,p_target_revision,p_version,p_candidate,p_candidate_sha,p_candidate_body,'mask');
 IF root.artifact_id IS NULL OR p_mask_body IS NULL OR octet_length(p_mask_body)<>root.bytes
 OR encode(sha256(convert_to(p_mask_body,'UTF8')),'hex') IS DISTINCT FROM root.sha256 THEN RETURN;END IF;
 body:=p_mask_body::jsonb;pin:=body->'artifact';
 IF body->'protected' IS DISTINCT FROM 'false'::jsonb OR jsonb_typeof(pin) IS DISTINCT FROM 'object'
 OR (SELECT count(*) FROM jsonb_object_keys(pin))<>2 OR coalesce(pin->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 SELECT a.created_by_task_id,a.produced_attempt,a.produced_fence INTO producer,attempt,fence FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=root.artifact_id;
 RETURN QUERY SELECT 'NpyArray'::text,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(pin->>'artifact_id')::uuid AND a.sha256=pin->>'sha256'
 AND a.created_by_task_id=producer AND a.produced_attempt=attempt AND a.produced_fence=fence AND a.output_role='mask_pixels'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/x-npy' AND a.bytes BETWEEN 32 AND 800016384
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.semantic_approved_mask_binary(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_mask_binary(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_approved_mask_binary(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
