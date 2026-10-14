-- +goose Up
-- Private current-authority witness; does not finalize or release reservations.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_failure_witness(p_expansion uuid,p_token uuid,p_slot integer) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE plan jsonb;frozen jsonb;request jsonb;observation jsonb;e photo.graph_expansions%ROWTYPE;
 w photo.v2_workflows%ROWTYPE;package_id uuid;reason text;required boolean;allow_omitted boolean;
BEGIN
 IF p_slot IS NULL OR p_slot NOT BETWEEN 0 AND 79999 THEN RETURN NULL;END IF;
 plan:=photo.v30_delivery_expansion_plan(p_expansion,p_token);
 IF plan IS NULL THEN RETURN NULL;END IF;
 frozen:=(plan->'proof'->>'frozen_input_jcs')::jsonb;
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF request IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion FOR UPDATE;
 IF NOT FOUND OR e.state<>'open' OR e.expansion_token IS DISTINCT FROM p_token
 OR e.expansion_lease_until<=clock_timestamp() OR p_slot>=e.cursor::integer THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=e.tenant_id AND run_id=e.run_id FOR SHARE;
 SELECT id INTO package_id FROM photo.delivery_packages WHERE tenant_id=e.tenant_id AND workflow_id=w.id
 AND project_id=w.project_id AND snapshot_id=w.input_snapshot_id AND state='building' FOR UPDATE;
 IF package_id IS NULL THEN RETURN NULL;END IF;
 required:=(request->'preset'->>'required')::boolean;allow_omitted:=(frozen->'policy'->>'allow_omitted')::boolean;
 IF required IS NULL OR allow_omitted IS NULL OR (NOT required AND allow_omitted) THEN RETURN NULL;END IF;
 -- Neutral primitive also checks exact package required flag, child actor/project
 -- and adopted producer identity, independently of caller-supplied evidence.
 observation:=photo.v30_delivery_current_export_observation(e.tenant_id,e.run_id,package_id,request);
 IF observation IS NULL OR observation->>'run_state' NOT IN('completed','completed_with_errors','failed','cancelled') THEN RETURN NULL;END IF;
 IF observation->>'run_state'='completed' AND observation->>'export_state'='ready'
 AND (observation->>'artifact_ready')::boolean IS TRUE THEN RETURN NULL;END IF;
 reason:=CASE WHEN observation->>'run_state'='cancelled' THEN 'EXPORT_CANCELLED'
 WHEN observation->>'run_state'='completed' THEN 'EXPORT_NOT_READY' ELSE 'EXPORT_FAILED' END;
 IF e.expansion_lease_until<=clock_timestamp() THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('slot_index',p_slot,'child_run_id',request->>'child_run_id',
 'export_id',observation->>'export_id','reason',reason,'package_id',package_id,'tenant_id',e.tenant_id,'run_id',e.run_id);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_failure_witness(uuid,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_failure_witness(uuid,uuid,integer) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
