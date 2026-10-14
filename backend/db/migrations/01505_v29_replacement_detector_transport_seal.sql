-- +goose Up
-- Freeze the complete typed transport on a blocked task. No graph activation.
-- +goose StatementBegin
CREATE FUNCTION photo.seal_v29_replacement_detector_transport(p_tenant uuid,p_run uuid,p_proof jsonb,p_closure jsonb,p_witness jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;plan jsonb;params jsonb;profile jsonb;wire jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 -- Registration independently repeats source IO authority, exact replacement
 -- control bodies, workflow policy and complete typed rows under locks.
 IF photo.register_v29_replacement_detector_inputs(p_tenant,p_run,p_proof,p_closure,p_witness) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 plan:=(r.config_snapshot->>'v29_perspective_replacement_detection_jcs')::jsonb;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(plan->>'task_id')::uuid FOR UPDATE;
 IF NOT FOUND OR t.attempt<>0 OR t.state<>'blocked' OR t.cancel_requested OR t.canonical_result_id IS NOT NULL
 OR t.execution_profile ? 'v2_input_manifest' THEN RETURN false;END IF;
 SELECT jsonb_build_object('artifact_id',d.artifact_id,'sha256',d.sha256) INTO params
 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=t.id
 AND d.purpose='task_params' AND d.state='ready' AND d.artifact_id=t.params_artifact_id;
 IF params IS NULL THEN RETURN false;END IF;
 wire:=jsonb_build_array(p_closure->'manifest',params);
 profile:=(plan->>'profile_jcs')::jsonb || jsonb_build_object(
 'contracts_sha','341984c53d787175fb5a9eec4e5d55ab74e9cd293075add8965d6064aba2e815',
 'v2_input_manifest',p_closure->'manifest','v2_entry_count',p_closure->'entry_count','v2_entries_sha',p_closure->>'entries_sha',
 'runtime_lock_sha',(r.v29_replacement_detector_execution_policy::jsonb)->>'runtime_lock_sha',
 'execution_policy_sha',encode(sha256(convert_to(r.v29_replacement_detector_execution_policy,'UTF8')),'hex'));
 UPDATE photo.tasks SET inputs=wire,execution_profile=profile WHERE tenant_id=p_tenant AND id=t.id;
 IF photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'LINE_DETECTION_INPUT_SEAL_INCOMPLETE' USING ERRCODE='23514';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.seal_v29_replacement_detector_transport(uuid,uuid,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v29_replacement_detector_transport(uuid,uuid,jsonb,jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.seal_v29_replacement_detector_transport(uuid,uuid,jsonb,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
