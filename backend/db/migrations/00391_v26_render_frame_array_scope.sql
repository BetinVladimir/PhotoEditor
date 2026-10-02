-- +goose Up
-- Canonical WorkingImage child refs are validated by the trusted resolver.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_frame_array_scope(
 p_tenant uuid,p_project uuid,p_version uuid,p_root uuid,p_root_sha text,
 p_descriptor uuid,p_descriptor_sha text,p_analysis uuid,p_selected_ordinal integer,p_model_lock text,
 p_frame_ordinal integer,p_frame_version uuid,p_source uuid,p_source_sha text,
 p_array uuid,p_array_sha text,p_contract text,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_contract IN('WorkingPixels','WorkingAlpha')
 AND photo.v26_render_frame_source_scope(p_tenant,p_project,p_version,p_root,p_root_sha,
 p_descriptor,p_descriptor_sha,p_analysis,p_selected_ordinal,p_model_lock,
 p_frame_ordinal,p_frame_version,p_source,p_source_sha,p_actor,p_session)
 AND EXISTS(SELECT 1 FROM photo.artifacts source JOIN photo.artifacts child
 ON child.tenant_id=source.tenant_id AND child.created_by_task_id=source.created_by_task_id
 AND child.produced_attempt=source.produced_attempt AND child.produced_fence=source.produced_fence
 WHERE source.tenant_id=p_tenant AND source.id=p_source AND source.sha256=p_source_sha
 AND source.created_by_task_id IS NOT NULL AND source.produced_attempt IS NOT NULL AND source.produced_fence IS NOT NULL
 AND child.id=p_array AND child.sha256=p_array_sha AND child.bytes>0
 AND child.media_type='application/x-npy' AND child.state='ready' AND child.verified_at IS NOT NULL
 AND child.object_version IS NOT NULL AND child.object_version NOT IN('','null')
 AND (child.expires_at IS NULL OR child.expires_at>clock_timestamp())
 AND ((p_contract='WorkingPixels' AND child.output_role IN('working_rgb','pixels'))
 OR (p_contract='WorkingAlpha' AND child.output_role='alpha'))),false);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_frame_array_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,integer,text,integer,uuid,uuid,text,uuid,text,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_frame_array_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,integer,text,integer,uuid,uuid,text,uuid,text,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_frame_array_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,integer,text,integer,uuid,uuid,text,uuid,text,text,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
