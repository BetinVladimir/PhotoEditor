-- +goose Up
-- Scheduler-only receipt for a blocked, registered prepare input closure.
-- This is transport evidence; it does not authorize package admission/dispatch.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_delivery_profile_matches(p_actual jsonb,p_expected jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(jsonb_typeof(p_actual)='object' AND jsonb_typeof(p_expected)='object'
 AND p_actual ?& ARRAY['contracts_sha','v2_input_manifest','v2_entry_count','v2_entries_sha']
 AND jsonb_typeof(p_actual->'contracts_sha')='string' AND p_actual->>'contracts_sha' ~ '^[0-9a-f]{64}$'
 AND NOT (p_expected ?| ARRAY['contracts_sha','v2_input_manifest','v2_entry_count','v2_entries_sha'])
 AND (p_actual-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=p_expected,false)
$$;
ALTER FUNCTION photo.v30_delivery_profile_matches(jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_profile_matches(jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_prepare_transport_ready(p_tenant uuid,p_run uuid,p_task jsonb,p_transport jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;actual jsonb;
BEGIN
 IF jsonb_typeof(p_task) IS DISTINCT FROM 'object' OR jsonb_typeof(p_transport) IS DISTINCT FROM 'object'
 OR p_transport-'manifest'-'chunks'-'count'-'sha256'<>'{}'::jsonb THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'v2' OR r.status IS DISTINCT FROM 'draft' OR r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_task->>'id')::uuid FOR UPDATE;
 IF t.id IS NULL OR t.state IS DISTINCT FROM 'blocked' OR t.attempt<>0 OR t.cancel_requested
 OR t.operation IS DISTINCT FROM 'v2.delivery.prepare' OR t.operation_version IS DISTINCT FROM '1'
 OR t.node_key IS DISTINCT FROM 'v2.delivery.prepare:0' OR t.resource_class IS DISTINCT FROM 'cpu'
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND workflow_type='delivery') THEN RETURN false;END IF;
 actual:=jsonb_build_object('id',t.id,'node_key',t.node_key,'operation',t.operation,'operation_version',t.operation_version,
 'resource_class',t.resource_class,'params_artifact_id',t.params_artifact_id,
 'params_sha',(SELECT sha256 FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=t.id
  AND purpose='task_params' AND artifact_id=t.params_artifact_id AND state='ready'),
 'input_digest',t.input_digest,'execution_profile',p_task->'execution_profile');
 IF actual IS DISTINCT FROM p_task OR photo.v30_delivery_profile_matches(t.execution_profile,p_task->'execution_profile') IS DISTINCT FROM true
 OR t.execution_profile->'v2_input_manifest' IS DISTINCT FROM p_transport->'manifest'
 OR t.execution_profile->'v2_entry_count' IS DISTINCT FROM p_transport->'count'
 OR t.execution_profile->>'v2_entries_sha' IS DISTINCT FROM p_transport->>'sha256'
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN (SELECT coalesce(jsonb_agg(jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) ORDER BY ordinal),'[]'::jsonb)
 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=t.id AND contract='InputClosureChunk')=p_transport->'chunks';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_delivery_prepare_transport_ready(uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_prepare_transport_ready(uuid,uuid,jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_prepare_transport_ready(uuid,uuid,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
