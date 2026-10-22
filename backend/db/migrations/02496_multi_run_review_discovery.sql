-- +goose Up
-- Read-only history discovery. A run ID is a lookup, never an admission authority.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_multi_run_review_scope(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;plan jsonb;receipt jsonb;params jsonb;project_id uuid;reference_asset uuid;producer_state text;adopted record;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
 AND kind='v2' AND graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR r.config_snapshot->'v29_multi_plan' IS NULL OR r.v29_multi_descriptor_jcs IS NULL THEN RETURN NULL;END IF;
 plan:=photo.v29_multi_execution_plan(p_tenant,p_run);
 IF plan IS NULL OR plan->>'operation' IS NULL OR plan->>'operation' NOT IN('optical.panorama','optical.focus_stack')
 OR plan->>'actor_id' IS DISTINCT FROM r.created_by::text OR plan->>'run_id' IS DISTINCT FROM p_run::text
 OR plan->>'snapshot_id' IS DISTINCT FROM r.input_snapshot_id::text THEN RETURN NULL;END IF;
 project_id:=(plan->>'project_id')::uuid;
 IF photo.v2_editor_authorized(p_tenant,project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 -- The original producer receipt repeats qualification, exact input inventory,
 -- immutable source scope and original issuer session; current reader auth is separate.
 -- Claim cache expiry must not erase a persisted run's history. The private
 -- historical reader checks the sealed receipt and all current source gates;
 -- this API wrapper cannot recreate a claim, admit a task or renew execution.
 SELECT state INTO producer_state FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(plan->>'task_id')::uuid FOR SHARE;
 IF producer_state='succeeded' THEN
  -- Completed project collaboration follows the existing adopted authority,
  -- which does not depend on the original author's login session remaining live.
  SELECT * INTO adopted FROM photo.v29_adopted_multi_binding(p_tenant,project_id,(plan->>'task_id')::uuid,p_actor,p_session,plan->>'operation');
  IF NOT FOUND OR adopted.run_id IS DISTINCT FROM p_run
  OR adopted.workflow_id::text IS DISTINCT FROM plan->>'workflow_id'
  OR adopted.snapshot_id::text IS DISTINCT FROM plan->>'snapshot_id'
  OR adopted.params_jcs IS DISTINCT FROM plan->>'params_jcs' THEN RETURN NULL;END IF;
  receipt:=r.v29_multi_queued_receipt;
 ELSE
  receipt:=photo.read_v29_multi_queued_receipt_pre_claim(p_tenant,p_run,r.created_by,(plan->>'auth_session_id')::uuid,plan);
 END IF;
 IF receipt IS NULL OR receipt->'schema_version' IS DISTINCT FROM '1'::jsonb OR receipt->>'state' IS DISTINCT FROM 'queued'
 OR receipt->>'operation' IS DISTINCT FROM plan->>'operation'
 OR receipt->>'actor_id' IS DISTINCT FROM r.created_by::text
 OR receipt->>'auth_session_id' IS DISTINCT FROM plan->>'auth_session_id'
 OR receipt->>'descriptor_sha' IS DISTINCT FROM encode(sha256(convert_to(r.v29_multi_descriptor_jcs,'UTF8')),'hex')
 OR receipt->>'run_id' IS DISTINCT FROM p_run::text
 OR receipt->>'workflow_id' IS DISTINCT FROM plan->>'workflow_id'
 OR receipt->>'task_id' IS DISTINCT FROM plan->>'task_id' THEN RETURN NULL;END IF;
 params:=(plan->>'params_jcs')::jsonb;
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' OR octet_length(plan->>'params_jcs') NOT BETWEEN 2 AND 65536
 OR encode(sha256(convert_to(plan->>'params_jcs','UTF8')),'hex') IS DISTINCT FROM plan->>'params_sha'
 OR jsonb_typeof(params->'frames') IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 SELECT asset_id INTO reference_asset FROM photo.asset_versions WHERE tenant_id=p_tenant
 AND id=(params->'frames'->((params->>'reference_ordinal')::integer)->>'asset_version_id')::uuid FOR SHARE;
 IF reference_asset IS NULL THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('tenant_id',p_tenant,'project_id',project_id,'run_id',p_run,'reference_asset_id',reference_asset,
 'workflow_id',plan->>'workflow_id','task_id',plan->>'task_id','operation',plan->>'operation',
 'intent',jsonb_build_object('schema_version',1,'snapshot_id',plan->>'snapshot_id','params',params));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_multi_run_review_scope(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_multi_run_review_scope(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_scheduler,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.read_v29_multi_run_review_scope(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
