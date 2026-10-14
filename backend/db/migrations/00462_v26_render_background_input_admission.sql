-- +goose Up
ALTER FUNCTION photo.v26_render_direct_inputs_ready(uuid,uuid,uuid,uuid,uuid,jsonb)
 RENAME TO v26_render_direct_inputs_before_background;
REVOKE ALL ON FUNCTION photo.v26_render_direct_inputs_before_background(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_direct_inputs_ready(p_tenant uuid,p_task uuid,p_project uuid,p_actor uuid,p_session uuid,p_body jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE stored jsonb; pin jsonb;
BEGIN
 IF NOT photo.v26_render_direct_inputs_before_background(p_tenant,p_task,p_project,p_actor,p_session,p_body) THEN RETURN false;END IF;
 SELECT u.render_background_asset INTO stored
 FROM photo.tasks t JOIN photo.v2_draft_uploads u ON u.tenant_id=t.tenant_id AND u.artifact_id=t.params_artifact_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND u.project_id=p_project
  AND u.state='ready' AND u.render_registered_at IS NOT NULL FOR SHARE OF u;
 pin:=p_body->'background_asset';
 IF pin IS NULL THEN RETURN stored IS NULL;END IF;
 IF stored IS NULL OR stored IS DISTINCT FROM pin
  OR NOT photo.v26_render_background_binding_ready(p_tenant,p_project,pin) THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.project_id=p_project
  AND i.task_id=p_task AND i.contract='StyleBackgroundAsset'
  AND i.artifact_id::text=pin->'source'->>'artifact_id' AND i.sha256=pin->'source'->>'sha256');
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_direct_inputs_ready(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_direct_inputs_ready(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
