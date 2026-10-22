-- +goose Up
-- Advisory review kind source, independent of target/preview availability.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_review_intent_object(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 PERFORM 1 FROM photo.semantic_review_identity(p_tenant,p_intent,p_actor,p_session);
 RETURN QUERY SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.semantic_intents i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id
 AND a.id=i.intent_artifact_id AND a.sha256=i.intent_sha
 WHERE i.tenant_id=p_tenant AND i.id=p_intent
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.bytes BETWEEN 2 AND 1048576 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
END $$;
ALTER FUNCTION photo.semantic_review_intent_object(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_review_intent_object(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_review_intent_object(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
