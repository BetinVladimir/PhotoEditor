-- +goose Up
-- Private plan resolver. Source mapping/lease/adoption checks remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_execution_plan(p_tenant uuid,p_run uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;packet jsonb;plan jsonb;identity jsonb;receipt jsonb;identity_jcs text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND kind='v2' AND graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF r.config_snapshot ? 'v29_multi_plan' THEN
 IF r.config_snapshot ? 'v29_negative_replacement' OR r.config_snapshot ? 'v29_negative_replacement_draft_jcs' THEN RETURN NULL;END IF;
 RETURN r.config_snapshot->'v29_multi_plan';
 END IF;
 IF r.config_snapshot->>'v29_negative_replacement_draft_jcs' IS NULL THEN RETURN NULL;END IF;
 packet:=(r.config_snapshot->>'v29_negative_replacement_draft_jcs')::jsonb;plan:=packet->'draft';identity:=packet->'identity';
 IF packet->'schema_version' IS DISTINCT FROM '2'::jsonb
 OR identity->>'operation' IS NULL OR identity->>'operation' NOT IN('optical.panorama','optical.focus_stack')
 OR plan->>'operation' IS DISTINCT FROM identity->>'operation'
 OR plan->>'tenant_id' IS DISTINCT FROM p_tenant::text OR identity->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR plan->>'run_id' IS DISTINCT FROM p_run::text OR identity->>'run_id' IS DISTINCT FROM p_run::text
 OR plan->>'project_id' IS DISTINCT FROM identity->>'project_id'
 OR plan->>'task_id' IS DISTINCT FROM identity->>'task_id'
 OR plan->>'workflow_id' IS DISTINCT FROM identity->>'workflow_id'
 OR plan->>'params_document_id' IS DISTINCT FROM identity->>'params_document_id'
 OR plan->>'params_sha' IS DISTINCT FROM identity->>'params_sha'
 OR plan->>'snapshot_id' IS DISTINCT FROM r.input_snapshot_id::text
 OR plan->>'actor_id' IS DISTINCT FROM r.created_by::text
 OR photo.v29_multi_replacement_inventory_valid(r.config_snapshot->>'v29_negative_replacement_draft_jcs') IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT '{'||string_agg(to_jsonb(key)::text||':'||value::text,',' ORDER BY key COLLATE "C")||'}' INTO identity_jcs FROM jsonb_each(identity);
 IF encode(sha256(convert_to(identity_jcs,'UTF8')),'hex') IS DISTINCT FROM packet->>'identity_sha' THEN RETURN NULL;END IF;
 receipt:=photo.read_v29_multi_replacement_queued_receipt(p_tenant,p_run,(plan->>'actor_id')::uuid,
 (plan->>'auth_session_id')::uuid,r.config_snapshot->>'v29_negative_replacement_draft_jcs');
 IF receipt IS NULL THEN RETURN NULL;END IF;
 RETURN plan;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_multi_execution_plan(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_execution_plan(uuid,uuid) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
