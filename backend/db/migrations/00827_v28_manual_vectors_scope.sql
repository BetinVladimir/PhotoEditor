-- +goose Up
-- Relational locator check only. Original Params and vector bytes must prove
-- the vectors field, canvas, source, geometry and SHA independently.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_manual_vectors_scope(p_tenant uuid,p_project uuid,
 p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb,
 p_target uuid,p_target_sha text,p_guard uuid,p_guard_sha text,
 p_target_pixel uuid,p_target_pixel_sha text,p_guard_pixel uuid,p_guard_pixel_sha text,
 p_root uuid,p_root_sha text,p_params uuid,p_params_sha text,p_vector uuid,p_vector_sha text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT COALESCE(p_vector IS NOT NULL AND p_vector_sha ~ '^[0-9a-f]{64}$'
  AND p_vector::text IS DISTINCT FROM (p_base->'source'->>'artifact_id')
  AND p_vector NOT IN(p_root,p_params,
   p_target,p_guard,p_target_pixel,p_guard_pixel)
  AND EXISTS(
   SELECT 1 FROM photo.v28_manual_pair_origin(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base,
    p_target,p_target_sha,p_guard,p_guard_sha,p_target_pixel,p_target_pixel_sha,p_guard_pixel,p_guard_pixel_sha) origin
   JOIN photo.artifacts root ON root.tenant_id=p_tenant AND root.id=origin.root_id AND root.sha256=origin.root_sha
   JOIN photo.tasks t ON t.tenant_id=root.tenant_id AND t.id=root.created_by_task_id
   JOIN photo.interactive_tasks i ON i.tenant_id=t.tenant_id AND i.task_id=t.id
   JOIN photo.artifacts vector ON vector.tenant_id=t.tenant_id AND vector.id=p_vector AND vector.sha256=p_vector_sha
   WHERE origin.root_id=p_root AND origin.root_sha=p_root_sha
    AND origin.params_id=p_params AND origin.params_sha=p_params_sha
    AND jsonb_typeof(t.inputs)='array' AND jsonb_typeof(i.declared_inputs)='array'
    AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_vector,'sha256',p_vector_sha))
    AND i.declared_inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_vector,'sha256',p_vector_sha))
    AND vector.state='ready' AND vector.verified_at IS NOT NULL
    AND vector.object_version IS NOT NULL AND vector.object_version NOT IN('','null')
    AND (vector.expires_at IS NULL OR vector.expires_at>clock_timestamp())
    AND vector.media_type='application/json' AND vector.bytes BETWEEN 2 AND 1048576),false);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_manual_vectors_scope(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_manual_vectors_scope(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_manual_vectors_scope(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
