-- +goose Up
-- Read committed Params without creating runs, reservations or object writes.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_perspective_prepared_params(p_tenant uuid,p_actor uuid,p_session uuid,p_plan text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE plan jsonb;stored jsonb;d photo.backend_documents%ROWTYPE;v_run uuid;v_task uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_plan IS NULL OR octet_length(p_plan) NOT BETWEEN 2 AND 262144 THEN RETURN NULL;END IF;
 plan:=p_plan::jsonb;v_run:=(plan->>'run_id')::uuid;v_task:=(plan->>'task_id')::uuid;
 IF plan->>'tenant_id' IS DISTINCT FROM p_tenant::text OR plan->>'actor_id' IS DISTINCT FROM p_actor::text
 OR plan->>'auth_session_id' IS DISTINCT FROM p_session::text THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT config_snapshot->'v29_perspective_plan' INTO stored FROM photo.runs
 WHERE tenant_id=p_tenant AND id=v_run AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR UPDATE;
 IF stored IS NULL OR stored IS DISTINCT FROM plan
 OR photo.v29_perspective_params_document_scope(p_tenant,v_run,v_task,'task_params',plan->>'params_sha',octet_length(plan->>'params_jcs'),jsonb_build_object('plan',plan,'params_jcs',plan->>'params_jcs')) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=v_run AND owner_id=v_task AND purpose='task_params'
 AND state='ready' AND artifact_id=(plan->>'params_document_id')::uuid AND sha256=plan->>'params_sha' AND bytes=octet_length(plan->>'params_jcs') FOR SHARE;
 IF d.id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket
 AND a.object_version=d.object_version AND a.object_version NOT IN('','null') AND a.media_type='application/json' AND a.output_role='task_params'
 AND a.object_key='tenants/'||p_tenant||'/runs/'||v_run||'/documents/'||d.artifact_id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('artifact_id',d.artifact_id,'sha256',d.sha256);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_perspective_prepared_params(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_perspective_prepared_params(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_perspective_prepared_params(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
