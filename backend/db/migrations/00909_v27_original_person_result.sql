-- +goose Up
-- Source-wide person evidence alone cannot continue an original native parent.
-- Read-only exact-child check; no queue, metadata or artifact mutation.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_original_person_result(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid,p_root uuid,p_root_sha text,p_pixels uuid,p_pixels_sha text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(EXISTS(
 SELECT 1 FROM photo.semantic_native_input_saved_stage(p_tenant,p_project,p_actor,p_session,p_parent,
 photo.semantic_uuid_v5(p_parent,'semantic.mask.person:reference'),'mask.person') saved
 JOIN photo.tasks task ON task.tenant_id=p_tenant AND task.id=saved.task_id AND task.id=p_task
 JOIN photo.runs run_row ON run_row.tenant_id=task.tenant_id AND run_row.id=task.run_id
 JOIN photo.artifacts root ON root.tenant_id=task.tenant_id AND root.created_by_task_id=task.id AND root.id=p_root AND root.sha256=p_root_sha
 JOIN photo.artifacts pixels ON pixels.tenant_id=task.tenant_id AND pixels.created_by_task_id=task.id AND pixels.id=p_pixels AND pixels.sha256=p_pixels_sha
 WHERE run_row.graph_sealed AND task.state='succeeded'
 AND run_row.config_snapshot->'v27_person_prepare'->>'request_sha'=p_request_sha
 AND photo.semantic_proposal_person_mask_origin(p_tenant,p_project,p_actor,p_session,
 (run_row.config_snapshot->'v27_person_prepare'->'source'->>'asset_version_id')::uuid,
 (run_row.config_snapshot->'v27_person_prepare'->'source'->'source'->>'artifact_id')::uuid,
 run_row.config_snapshot->'v27_person_prepare'->'source'->'source'->>'sha256',p_root,p_root_sha,p_pixels,p_pixels_sha)
 ),false);
$$;
ALTER FUNCTION photo.semantic_native_original_person_result(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_original_person_result(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_original_person_result(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
