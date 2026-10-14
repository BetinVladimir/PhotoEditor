-- +goose Up
-- Backend-only scoped metadata for the selected protected root. Binary producer
-- origin remains separately required after decoding exact root bytes.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_protection_object(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_target uuid,p_target_revision bigint,p_version uuid,p_candidate uuid,p_candidate_sha text,p_root uuid,p_root_sha text)
RETURNS TABLE(contract text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin record;
BEGIN
 SELECT * INTO pin FROM photo.semantic_approved_protection_pin(p_tenant,p_intent,p_actor,p_session,p_revision,p_target,p_target_revision,p_version,p_candidate,p_candidate_sha);
 IF pin.artifact_id IS NULL OR pin.artifact_id IS DISTINCT FROM p_root OR pin.sha256 IS DISTINCT FROM p_root_sha THEN RETURN;END IF;
 RETURN QUERY SELECT 'MaskRef'::text,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_root AND a.sha256=p_root_sha
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 2 AND 8388608 AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
END $$;
ALTER FUNCTION photo.semantic_approved_protection_object(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_protection_object(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_approved_protection_object(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
