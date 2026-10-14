-- +goose Up
-- Read-only current replacement descriptor; no artifact locator or admission rights.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_perspective_replacement_review_scope(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_original uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE original_run uuid;namespace uuid;replacement_run uuid;replacement_task uuid;plan jsonb;source jsonb;packet jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_hash IS NULL OR p_hash !~ '^[0-9a-f]{64}$'
 OR photo.v29_negative_invalidation_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT t.run_id INTO original_run FROM photo.tasks t JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 WHERE t.tenant_id=p_tenant AND t.id=p_original AND t.operation='optical.perspective' AND w.project_id=p_project;
 IF NOT FOUND THEN RETURN NULL;END IF;
 namespace:=photo.v30_uuid5(p_tenant,'V29.NegativeReplacement/v2:'||p_project::text||':'||p_review::text||':'||p_hash);
 replacement_run:=photo.v30_uuid5(namespace,'run:'||original_run::text||':'||p_original::text);
 replacement_task:=photo.v30_uuid5(namespace,'task:'||p_original::text);
 IF photo.v29_negative_replacement_member(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_original,replacement_task,replacement_run) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 plan:=photo.v29_perspective_execution_plan(p_tenant,replacement_run);
 SELECT (r.config_snapshot->>'v29_negative_replacement_draft_jcs')::jsonb INTO packet
 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=replacement_run;
 IF plan IS NULL OR plan->>'task_id' IS DISTINCT FROM replacement_task::text
 OR packet->'identity'->>'review_id' IS DISTINCT FROM p_review::text
 OR packet->'identity'->>'decision_hash' IS DISTINCT FROM p_hash
 OR packet->'identity'->>'original_task_id' IS DISTINCT FROM p_original::text
 OR packet->'identity'->>'original_run_id' IS DISTINCT FROM original_run::text THEN RETURN NULL;END IF;
 source:=photo.v29_perspective_replacement_source(p_tenant,p_project,p_actor,p_session,replacement_task);
 IF source IS NULL THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('tenant_id',p_tenant,'project_id',p_project,'review_id',p_review,'decision_hash',p_hash,
 'original_run_id',original_run,'original_task_id',p_original,'run_id',replacement_run,
 'workflow_id',plan->>'workflow_id','task_id',replacement_task,'base',((plan->>'params_jcs')::jsonb)->'base');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_perspective_replacement_review_scope(uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_perspective_replacement_review_scope(uuid,uuid,uuid,uuid,uuid,text,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.read_v29_perspective_replacement_review_scope(uuid,uuid,uuid,uuid,uuid,text,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
