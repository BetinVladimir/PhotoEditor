-- +goose Up
-- Extend existing replacement source/adoption/preview plan authority to auto mode.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v29_perspective_execution_plan(p_tenant uuid,p_run uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;packet jsonb;plan jsonb;receipt jsonb;bucket text;
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
 IF r.config_snapshot ? 'v29_auto_perspective_segments' THEN
 IF ((plan->>'params_jcs')::jsonb)->>'mode' IS DISTINCT FROM 'auto' THEN RETURN NULL;END IF;
 SELECT a.bucket INTO bucket FROM photo.artifacts a WHERE a.tenant_id=p_tenant
 AND a.id=(((plan->>'params_jcs')::jsonb)->'lines'->>'artifact_id')::uuid;
 receipt:=photo.read_v29_auto_perspective_replacement_queued_receipt(p_tenant,p_run,
 (plan->>'actor_id')::uuid,(plan->>'auth_session_id')::uuid,r.config_snapshot->>'v29_negative_replacement_draft_jcs',
 r.config_snapshot->'v29_auto_perspective_segments',bucket);
 ELSE
 receipt:=photo.read_v29_manual_perspective_replacement_queued_receipt(p_tenant,p_run,
 (plan->>'actor_id')::uuid,(plan->>'auth_session_id')::uuid,r.config_snapshot->>'v29_negative_replacement_draft_jcs');
 END IF;
 IF receipt IS NULL THEN RETURN NULL;END IF;
 RETURN plan;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_perspective_execution_plan(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_execution_plan(uuid,uuid) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
