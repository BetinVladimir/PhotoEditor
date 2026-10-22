-- +goose Up
-- Read-only source-bound canonical landmark candidates. No latest-result choice:
-- consumers must reject competing catalogs and validate physical face geometry.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_retouch_preview_landmark_catalog(p_tenant uuid,p_intent uuid,p_target uuid,
 p_actor uuid,p_session uuid,p_revision bigint,p_target_revision bigint,p_plan_sha text,p_candidate_sha text,p_source jsonb)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.semantic_preapply_preview_current(p_tenant,p_intent,p_target,p_actor,p_session,
 p_revision,p_target_revision,p_plan_sha,p_candidate_sha) IS DISTINCT FROM true
 OR jsonb_typeof(p_source) IS DISTINCT FROM 'object' THEN
 RAISE EXCEPTION 'SEMANTIC_RETOUCH_FACE_SCOPE_STALE' USING ERRCODE='42501';END IF;
 SELECT i.project_id INTO project FROM photo.semantic_intents i
 JOIN photo.semantic_targets target ON target.tenant_id=i.tenant_id AND target.intent_id=i.id
 WHERE i.tenant_id=p_tenant AND i.id=p_intent AND target.id=p_target
 AND target.asset_version_id::text=p_source->>'asset_version_id';
 IF project IS NULL THEN
 RAISE EXCEPTION 'SEMANTIC_RETOUCH_FACE_SCOPE_STALE' USING ERRCODE='42501';END IF;
 RETURN QUERY SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.output_role='face_landmarks'
 AND a.state='ready' AND a.media_type='application/json'
 AND photo.semantic_retouch_preview_landmark_origin(p_tenant,project,p_actor,p_session,p_source,a.id,a.sha256) IS TRUE
 ORDER BY a.id LIMIT 65;
END $$;
ALTER FUNCTION photo.semantic_retouch_preview_landmark_catalog(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_retouch_preview_landmark_catalog(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.semantic_retouch_preview_landmark_catalog(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
