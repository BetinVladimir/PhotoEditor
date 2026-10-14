-- +goose Up
-- Authenticated exact binary metadata; not numerical NPY validation. Backend
-- must stream with declared bounds and repeat scope after IO/final admission.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_protection_binary(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_target uuid,p_target_revision bigint,p_version uuid,p_candidate uuid,p_candidate_sha text,p_root uuid,p_root_sha text,p_binary uuid,p_binary_sha text,p_body text)
RETURNS TABLE(contract text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT photo.semantic_approved_protection_origin(p_tenant,p_intent,p_actor,p_session,p_revision,p_target,p_target_revision,p_version,p_candidate,p_candidate_sha,p_root,p_root_sha,p_binary,p_binary_sha,p_body) THEN RETURN;END IF;
 RETURN QUERY SELECT 'NpyArray'::text,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_binary AND a.sha256=p_binary_sha
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 32 AND 800016384
 AND a.media_type IN('application/octet-stream','application/x-npy')
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
END $$;
ALTER FUNCTION photo.semantic_approved_protection_binary(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_protection_binary(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_approved_protection_binary(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
