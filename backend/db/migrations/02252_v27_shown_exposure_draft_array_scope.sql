-- +goose Up
-- Shown-copy pixels are newly produced; alpha remains an exact retained input.
-- Keep historical scope behavior for every other source.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_version_array_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_artifact uuid,p_sha text,p_contract text,p_actor uuid,p_session uuid,p_parent uuid,p_parent_sha text)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND p_contract IN('WorkingPixels','WorkingAlpha') AND p_sha~'^[0-9a-f]{64}$'
 AND p_parent_sha~'^[0-9a-f]{64}$' AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND EXISTS(
 SELECT 1 FROM photo.asset_versions v
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.semantic_targets target ON target.tenant_id=v.tenant_id AND target.asset_version_id=v.parent_asset_version_id
 AND target.child_run_id=v.created_by_run_id AND target.project_id=asset.project_id
 JOIN LATERAL photo.semantic_shown_applied_image(p_tenant,target.id,p_actor,p_session) proof ON true
 JOIN photo.artifacts root ON root.tenant_id=v.tenant_id AND root.id=v.master_artifact_id AND root.sha256=v.content_hash
 JOIN photo.artifacts child ON child.tenant_id=v.tenant_id AND child.id=p_artifact AND child.sha256=p_sha
 WHERE v.tenant_id=p_tenant AND v.id=p_version AND asset.project_id=p_project
 AND asset.deleted_at IS NULL AND asset.upload_state='verified' AND v.review_state='accepted'
 AND v.id=photo.semantic_uuid_v5(proof.task_id,'V27.ShownAppliedVersion')
 AND v.master_artifact_id=proof.artifact_id AND v.content_hash=proof.sha256
 AND root.id=p_parent AND root.sha256=p_parent_sha AND root.output_role='working_image'
 AND root.state='ready' AND root.verified_at IS NOT NULL AND root.media_type='application/json'
 AND root.object_version IS NOT NULL AND root.object_version NOT IN('','null')
 AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.asset_version_sources lineage WHERE lineage.tenant_id=v.tenant_id AND lineage.version_id=v.id
 AND lineage.source_version_id=v.parent_asset_version_id AND lineage.role='semantic_source' AND lineage.ordinal=0)
 AND v.metadata->CASE p_contract WHEN 'WorkingPixels' THEN 'pixels' ELSE 'alpha' END
 =jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha)
 AND child.state='ready' AND child.verified_at IS NOT NULL AND child.object_version IS NOT NULL
 AND child.object_version NOT IN('','null') AND (child.expires_at IS NULL OR child.expires_at>clock_timestamp())
 AND child.bytes BETWEEN 1 AND 2400065536 AND child.media_type IN('application/x-npy','application/octet-stream')
 AND ((p_contract='WorkingPixels' AND child.created_by_task_id=proof.task_id AND child.output_role='pixels'
 AND EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
 WHERE t.tenant_id=p_tenant AND t.id=proof.task_id AND z.id=proof.result_id
 AND child.produced_attempt=t.attempt AND child.produced_fence=t.fencing_token))
 OR (p_contract='WorkingAlpha' AND EXISTS(SELECT 1 FROM photo.v2_task_inputs input
 WHERE input.tenant_id=p_tenant AND input.project_id=p_project AND input.task_id=proof.task_id
 AND input.contract='NpyArray' AND input.artifact_id=child.id AND input.sha256=child.sha256)))),false);
$$;
ALTER FUNCTION photo.semantic_shown_version_array_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_version_array_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_shown_version_array_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) TO photo_api;
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text)
 RENAME TO v2_draft_artifact_scope_pre_shown_version;
CREATE FUNCTION photo.v2_draft_artifact_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_artifact uuid,p_sha text,p_contract text,p_actor uuid,p_session uuid,p_parent uuid,p_parent_sha text)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v2_draft_artifact_scope_pre_shown_version(p_tenant,p_project,p_version,p_artifact,p_sha,p_contract,p_actor,p_session,p_parent,p_parent_sha)
 OR photo.semantic_shown_version_array_scope(p_tenant,p_project,p_version,p_artifact,p_sha,p_contract,p_actor,p_session,p_parent,p_parent_sha);
$$;
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
