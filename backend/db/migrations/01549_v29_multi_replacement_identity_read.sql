-- +goose Up
-- Current identity metadata only; adopted result/source authority is separate.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_multi_replacement_identity(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_task uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;plan jsonb;identity jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT run.* INTO r FROM photo.tasks task JOIN photo.runs run ON run.tenant_id=task.tenant_id AND run.id=task.run_id
 WHERE task.tenant_id=p_tenant AND task.id=p_task AND task.operation IN('optical.panorama','optical.focus_stack');
 IF r.id IS NULL OR NOT(r.config_snapshot ? 'v29_negative_replacement_draft_jcs') THEN RETURN NULL;END IF;
 plan:=photo.v29_multi_execution_plan(p_tenant,r.id);
 IF plan IS NULL OR plan->>'task_id' IS DISTINCT FROM p_task::text OR plan->>'project_id' IS DISTINCT FROM p_project::text THEN RETURN NULL;END IF;
 identity:=((r.config_snapshot->>'v29_negative_replacement_draft_jcs')::jsonb)->'identity';
 RETURN jsonb_build_object('tenant_id',p_tenant,'project_id',p_project,'review_id',identity->>'review_id',
 'decision_hash',identity->>'decision_hash','original_run_id',identity->>'original_run_id','original_task_id',identity->>'original_task_id');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_multi_replacement_identity(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_multi_replacement_identity(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_multi_replacement_identity(uuid,uuid,uuid,uuid,uuid) TO photo_api,photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
