-- +goose Up
-- Backend Params must be readable before the blocked task shell is created.
-- +goose StatementBegin
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) RENAME TO v2_draft_artifact_scope_pre_v28;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope_pre_v28(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM photo_api;
CREATE FUNCTION photo.v2_draft_artifact_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_artifact uuid,p_sha text,p_contract text,p_actor uuid,p_session uuid,p_parent uuid,p_parent_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.v2_draft_artifact_scope_pre_v28(p_tenant,p_project,p_version,p_artifact,p_sha,p_contract,p_actor,p_session,p_parent,p_parent_sha) IS TRUE THEN RETURN true;END IF;
 IF p_contract IS DISTINCT FROM 'Params' OR p_parent IS NOT NULL OR p_parent_sha IS NOT NULL
  OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.backend_documents d
  JOIN photo.runs r ON r.tenant_id=d.tenant_id AND r.id=d.run_id
  JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  WHERE d.tenant_id=p_tenant AND d.artifact_id=p_artifact AND d.sha256=p_sha AND d.state='ready' AND d.purpose='task_params'
   AND r.config_snapshot->'v28_local_portrait'->>'project_id'=p_project::text
   AND r.config_snapshot->'v28_local_portrait'->>'actor_id'=p_actor::text
   AND r.config_snapshot->'v28_local_portrait'->>'auth_session_id'=p_session::text
   AND r.config_snapshot->'v28_local_portrait'->'intent'->'binding'->>'asset_version_id'=p_version::text
   AND a.state='ready' AND a.sha256=p_sha AND a.bytes=d.bytes AND a.media_type='application/json'
   AND a.verified_at IS NOT NULL AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND photo.v28_local_params_document_scope(p_tenant,d.run_id,d.owner_id,d.purpose,d.sha256,d.bytes) IS TRUE);
END $$;
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
