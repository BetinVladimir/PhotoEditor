-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_relight_analysis_stored_outcome(p_tenant uuid,p_project uuid,p_run uuid,p_actor uuid,p_session uuid,p_intent jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;plan jsonb;config jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR NOT photo.v28_receipt_actor_live(p_tenant,p_project,p_actor,p_session) THEN RETURN jsonb_build_object('outcome','denied');END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN jsonb_build_object('outcome','denied');END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF NOT photo.v28_receipt_actor_live(p_tenant,p_project,p_actor,p_session) THEN RETURN jsonb_build_object('outcome','denied');END IF;
 IF r.id IS NULL THEN RETURN jsonb_build_object('outcome','absent');END IF;
 IF r.created_by IS DISTINCT FROM p_actor OR r.kind IS DISTINCT FROM 'v2' THEN RETURN jsonb_build_object('outcome','denied');END IF;
 plan:=r.config_snapshot->'v28_analysis_plan';config:=plan->'config';
 IF jsonb_typeof(plan) IS DISTINCT FROM 'object' OR jsonb_typeof(config) IS DISTINCT FROM 'object' THEN RETURN jsonb_build_object('outcome','denied');END IF;
 IF config->>'tenant_id' IS DISTINCT FROM p_tenant::text OR config->>'project_id' IS DISTINCT FROM p_project::text
 OR config->>'actor_id' IS DISTINCT FROM p_actor::text OR config->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR p_intent IS DISTINCT FROM jsonb_build_object('schema_version',1,'snapshot_id',config->'snapshot_id','base',config->'base','operation',plan->'operation') THEN RETURN jsonb_build_object('outcome','conflict');END IF;
 -- Stored execution profile is input to existing full validation, never an
 -- authority to start or publish work. No current deployment pin is required.
 RETURN photo.read_v28_relight_analysis_outcome(p_tenant,p_run,p_actor,p_session,plan);
END $$;
ALTER FUNCTION photo.read_v28_relight_analysis_stored_outcome(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_relight_analysis_stored_outcome(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_relight_analysis_stored_outcome(uuid,uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
