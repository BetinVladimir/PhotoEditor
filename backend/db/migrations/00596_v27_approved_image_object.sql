-- +goose Up
-- Authenticated target/reference WorkingImage metadata. Binary children remain
-- separately scoped and verified; JSON references alone grant no child access.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_image_object(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_target uuid,p_target_revision bigint,p_version uuid,p_candidate uuid,p_candidate_sha text,p_reference boolean,p_source uuid,p_source_sha text)
RETURNS TABLE(contract text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE version uuid;project uuid;
BEGIN
 PERFORM photo.lock_semantic_approved_params_publication(p_tenant,p_intent,p_actor,p_session,p_revision,p_target,p_target_revision,p_version,p_candidate,p_candidate_sha);
 IF p_reference IS NULL THEN RETURN;END IF;
 SELECT CASE WHEN p_reference THEN i.reference_version_id ELSE p_version END,i.project_id INTO version,project
 FROM photo.semantic_intents i WHERE i.tenant_id=p_tenant AND i.id=p_intent;
 RETURN QUERY SELECT 'WorkingImage'::text,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.asset_versions v JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id AND asset.project_id=project
 JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.master_artifact_id AND a.sha256=v.content_hash
 WHERE v.tenant_id=p_tenant AND v.id=version AND a.id=p_source AND a.sha256=p_source_sha
 AND asset.deleted_at IS NULL AND asset.upload_state='verified' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.bytes BETWEEN 2 AND 8388608 AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF v,asset,a;
END $$;
ALTER FUNCTION photo.semantic_approved_image_object(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,boolean,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_image_object(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,boolean,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_approved_image_object(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,boolean,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
