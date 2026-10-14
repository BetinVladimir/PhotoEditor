-- +goose Up
-- Resolve the dedicated replacement packet through historical receipt authority.
-- Existing source/version, producer, adoption and quality checks remain required.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_execution_plan(p_tenant uuid,p_run uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;packet jsonb;plan jsonb;receipt jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND kind='v2' AND graph_sealed AND NOT cancel_requested;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF r.config_snapshot ? 'v29_perspective_plan' THEN
 IF r.config_snapshot ? 'v29_negative_replacement' OR r.config_snapshot ? 'v29_negative_replacement_draft_jcs' THEN RETURN NULL;END IF;
 RETURN r.config_snapshot->'v29_perspective_plan';
 END IF;
 IF r.config_snapshot->>'v29_negative_replacement_draft_jcs' IS NULL THEN RETURN NULL;END IF;
 packet:=(r.config_snapshot->>'v29_negative_replacement_draft_jcs')::jsonb;plan:=packet->'draft';
 IF packet->'identity'->>'operation' IS DISTINCT FROM 'optical.perspective'
 OR plan->>'tenant_id' IS DISTINCT FROM p_tenant::text OR plan->>'run_id' IS DISTINCT FROM p_run::text
 OR plan->>'actor_id' IS DISTINCT FROM r.created_by::text THEN RETURN NULL;END IF;
 receipt:=photo.read_v29_manual_perspective_replacement_queued_receipt(p_tenant,p_run,
 (plan->>'actor_id')::uuid,(plan->>'auth_session_id')::uuid,r.config_snapshot->>'v29_negative_replacement_draft_jcs');
 IF receipt IS NULL THEN RETURN NULL;END IF;
 RETURN plan;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_perspective_execution_plan(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_execution_plan(uuid,uuid) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
DECLARE definition text;old_text text;
BEGIN
 definition:=pg_get_functiondef('photo.v29_adopted_perspective_binding(uuid,uuid,uuid,uuid,uuid,uuid)'::regprocedure);
 old_text:='(r.config_snapshot->''v29_perspective_plan''->>''params_jcs'')';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'PERSPECTIVE_EXECUTION_PARAMS_BASELINE_MISMATCH';END IF;
 definition:=replace(definition,old_text,'(photo.v29_perspective_execution_plan(p_tenant,r.id)->>''params_jcs'')');
 old_text:='r.config_snapshot->''v29_perspective_plan''->>''task_id''=t.id::text AND r.graph_sealed AND r.v29_perspective_queued_receipt IS NOT NULL';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'PERSPECTIVE_EXECUTION_RECEIPT_BASELINE_MISMATCH';END IF;
 definition:=replace(definition,old_text,'photo.v29_perspective_execution_plan(p_tenant,r.id)->>''task_id''=t.id::text AND r.graph_sealed AND (r.v29_perspective_queued_receipt IS NOT NULL OR r.v29_negative_replacement_queued_receipt IS NOT NULL)');
 EXECUTE definition;
 definition:=pg_get_functiondef('photo.v29_perspective_preview_source(uuid,uuid)'::regprocedure);
 old_text:='r.config_snapshot->''v29_perspective_plan'' AS body';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'PERSPECTIVE_PREVIEW_EXECUTION_PLAN_BASELINE_MISMATCH';END IF;
 EXECUTE replace(definition,old_text,'photo.v29_perspective_execution_plan(p_tenant,r.id) AS body');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
