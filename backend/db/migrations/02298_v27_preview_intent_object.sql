-- +goose Up
-- Read-only dispatch metadata under the same current preview authority fence.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preview_intent_object(p_tenant uuid,p_intent uuid,p_target uuid,
 p_actor uuid,p_session uuid,p_revision bigint,p_target_revision bigint,p_plan_sha text,p_candidate_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.semantic_intents i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id
 AND a.id=i.intent_artifact_id AND a.sha256=i.intent_sha
 WHERE i.tenant_id=p_tenant AND i.id=p_intent
 AND photo.semantic_preapply_preview_current(p_tenant,p_intent,p_target,p_actor,p_session,
 p_revision,p_target_revision,p_plan_sha,p_candidate_sha) IS TRUE
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.bytes BETWEEN 2 AND 1048576 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
ALTER FUNCTION photo.semantic_preview_intent_object(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preview_intent_object(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_preview_intent_object(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
