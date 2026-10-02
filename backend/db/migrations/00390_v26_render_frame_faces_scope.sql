-- +goose Up
-- The trusted resolver derives the face ref from canonical DescriptorShard bytes.
-- Faces must be staged by the same canonical describe attempt as that shard.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_frame_faces_scope(
 p_tenant uuid,p_project uuid,p_version uuid,p_root uuid,p_root_sha text,
 p_descriptor uuid,p_descriptor_sha text,p_analysis uuid,p_selected_ordinal integer,p_model_lock text,
 p_frame_ordinal integer,p_frame_version uuid,p_source uuid,p_source_sha text,
 p_faces uuid,p_faces_sha text,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(photo.v26_render_frame_source_scope(p_tenant,p_project,p_version,p_root,p_root_sha,
 p_descriptor,p_descriptor_sha,p_analysis,p_selected_ordinal,p_model_lock,
 p_frame_ordinal,p_frame_version,p_source,p_source_sha,p_actor,p_session)
 AND EXISTS(SELECT 1 FROM photo.artifacts descriptor
 JOIN photo.artifacts faces ON faces.tenant_id=descriptor.tenant_id
 AND faces.created_by_task_id=descriptor.created_by_task_id
 AND faces.produced_attempt=descriptor.produced_attempt AND faces.produced_fence=descriptor.produced_fence
 JOIN photo.output_uploads upload ON upload.tenant_id=faces.tenant_id AND upload.artifact_id=faces.id
 AND upload.task_id=faces.created_by_task_id AND upload.attempt=faces.produced_attempt AND upload.fence=faces.produced_fence
 WHERE descriptor.tenant_id=p_tenant AND descriptor.id=p_descriptor AND descriptor.sha256=p_descriptor_sha
 AND faces.id=p_faces AND faces.sha256=p_faces_sha AND faces.output_role='faces'
 AND faces.media_type='application/json' AND faces.state='ready' AND faces.verified_at IS NOT NULL
 AND faces.object_version IS NOT NULL AND faces.object_version NOT IN('','null')
 AND (faces.expires_at IS NULL OR faces.expires_at>clock_timestamp())
 AND upload.state='ready' AND upload.role='faces' AND upload.sha256=faces.sha256
 AND upload.object_version=faces.object_version AND upload.bytes=faces.bytes),false);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_frame_faces_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,integer,text,integer,uuid,uuid,text,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_frame_faces_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,integer,text,integer,uuid,uuid,text,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_frame_faces_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,integer,text,integer,uuid,uuid,text,uuid,text,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
