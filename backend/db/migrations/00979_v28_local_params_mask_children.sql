-- +goose Up
-- Bind nested MaskRef occurrences to the exact published saved local Params.
-- +goose StatementBegin
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) RENAME TO v2_draft_artifact_scope_pre_v28_local_children;
CREATE FUNCTION photo.v2_draft_artifact_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_artifact uuid,p_sha text,p_contract text,p_actor uuid,p_session uuid,p_parent uuid,p_parent_sha text)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v2_draft_artifact_scope_pre_v28_local_children(p_tenant,p_project,p_version,p_artifact,p_sha,p_contract,p_actor,p_session,p_parent,p_parent_sha)
 OR (p_contract='MaskRef' AND p_parent IS NOT NULL AND p_parent_sha ~ '^[0-9a-f]{64}$'
 AND photo.v2_draft_artifact_scope_pre_v28_local_children(p_tenant,p_project,p_version,p_artifact,p_sha,p_contract,p_actor,p_session,NULL,NULL) IS TRUE
 AND EXISTS(
 SELECT 1 FROM photo.backend_documents d
 JOIN photo.runs r ON r.tenant_id=d.tenant_id AND r.id=d.run_id
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256
 WHERE d.tenant_id=p_tenant AND d.artifact_id=p_parent AND d.sha256=p_parent_sha AND d.purpose='task_params' AND d.state='ready'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
  AND a.object_version=d.object_version AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND r.config_snapshot->'v28_local_portrait'->>'actor_id'=p_actor::text
  AND r.config_snapshot->'v28_local_portrait'->>'auth_session_id'=p_session::text
  AND r.config_snapshot->'v28_local_portrait'->>'project_id'=p_project::text
  AND r.config_snapshot->'v28_local_portrait'->'intent'->'binding'->>'asset_version_id'=p_version::text
  AND photo.v28_local_params_document_scope(p_tenant,r.id,d.owner_id,d.purpose,d.sha256,d.bytes) IS TRUE
  AND (r.config_snapshot->'v28_local_portrait'->'intent'->>'params_jcs')::jsonb @>
   jsonb_build_object(
    CASE WHEN EXISTS(SELECT 1 FROM photo.artifacts m WHERE m.tenant_id=p_tenant AND m.id=p_artifact AND m.output_role='mask_target_ref') THEN 'mask' ELSE 'protected' END,
    jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha))));
$$;
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
