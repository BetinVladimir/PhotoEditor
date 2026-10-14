-- +goose Up
-- Read-only replay of the stored202 body; never refreshes run/task state.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_lines_submission_response(p_tenant uuid,p_project uuid,p_run uuid,
 p_actor uuid,p_session uuid,p_key text,p_request text,p_policy text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;i photo.idempotency_records%ROWTYPE;r photo.runs%ROWTYPE;plan jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 8192 THEN RETURN NULL;END IF;
 request:=p_request::jsonb;
 IF photo.v28_local_portrait_source_scope(p_tenant,p_project,(request->>'snapshot_id')::uuid,p_actor,p_session,request->'base') IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO i FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='V29.PerspectiveLines/'||p_project::text AND key=p_key;
 IF NOT FOUND OR i.resource_id IS DISTINCT FROM p_run OR i.request_hash IS DISTINCT FROM encode(sha256(convert_to(p_request,'UTF8')),'hex')
 OR i.response_status IS DISTINCT FROM 202 OR i.response_body IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF NOT FOUND OR NOT r.v29_perspective_line_detection OR NOT r.graph_sealed OR r.created_by IS DISTINCT FROM p_actor
 OR r.v29_perspective_lines_execution_policy IS DISTINCT FROM p_policy
 OR r.v29_perspective_lines_admission_receipt IS DISTINCT FROM i.response_body THEN RETURN NULL;END IF;
 plan:=r.config_snapshot->'v29_perspective_line_detection';
 IF plan->>'project_id' IS DISTINCT FROM p_project::text OR plan->>'snapshot_id' IS DISTINCT FROM request->>'snapshot_id'
 OR plan->'base' IS DISTINCT FROM request->'base' THEN RETURN NULL;END IF;
 RETURN i.response_body;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_perspective_lines_submission_response(uuid,uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_submission_response(uuid,uuid,uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_lines_submission_response(uuid,uuid,uuid,uuid,uuid,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
