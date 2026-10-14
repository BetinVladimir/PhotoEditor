-- +goose Up
-- Bind worker qualification to the exact registered frozen Calibration ref.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v27_semantic_worker_qualification(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; slot photo.resource_slots%ROWTYPE; pin jsonb;
BEGIN
 IF p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF r.id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested OR r.status NOT IN('queued','running') THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 IF t.id IS NULL OR t.operation<>'semantic.transfer' OR t.operation_version<>'1' OR t.resource_class<>'gpu'
 OR t.state<>'running' OR t.cancel_requested OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.worker_id IS DISTINCT FROM p_worker OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
 OR t.deadline IS NULL OR t.deadline<=clock_timestamp() OR t.params_artifact_id IS DISTINCT FROM p_params
 OR t.input_digest IS DISTINCT FROM p_digest OR t.execution_profile->>'backend' IS DISTINCT FROM 'cuda'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN NULL;END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task FOR SHARE;
 IF slot.id IS NULL OR slot.fencing_token IS DISTINCT FROM p_fence OR slot.resource_class IS DISTINCT FROM 'gpu'
 OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp()
 OR NOT photo.worker_authorized(p_tenant,p_worker,'gpu',slot.node_id,t.operation,'cuda',t.execution_profile->>'model_lock_id') THEN RETURN NULL;END IF;
 PERFORM worker_id FROM photo.worker_registrations WHERE tenant_id=p_tenant AND worker_id=p_worker
 AND db_principal=session_user AND enabled AND v2_renderer_versions ? 'v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND sha256=p_sha
 AND state='ready' AND verified_at IS NOT NULL AND media_type='application/json' AND bytes BETWEEN 2 AND 1048576
 AND object_version IS NOT NULL AND object_version NOT IN('','null') AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF jsonb_typeof(r.config_snapshot->'v27_semantic'->'nodes') IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 IF (SELECT count(*) FROM jsonb_array_elements(r.config_snapshot->'v27_semantic'->'nodes') n
 WHERE n->>'task_id'=p_task::text AND n->>'params_artifact_id'=p_params::text AND n->>'params_sha'=p_sha)<>1
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND artifact_id=p_params AND sha256=p_sha AND contract='Params')
 OR NOT photo.v2_input_closure_ready(p_task) OR NOT photo.v27_semantic_frozen_qualification_ready(p_tenant,p_run) THEN RETURN NULL;END IF;
 pin:=r.config_snapshot->'v27_semantic'->'qualification';
 IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='Calibration')<>1
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='Calibration'
 AND artifact_id::text=pin->'calibration'->>'artifact_id' AND sha256=pin->'calibration'->>'sha256') THEN RETURN NULL;END IF;
 PERFORM id FROM photo.v2_qualification_policies WHERE id=(pin->>'policy_id')::uuid FOR SHARE;
 PERFORM id FROM photo.model_qualifications WHERE tenant_id=p_tenant AND id=(pin->>'qualification_id')::uuid FOR SHARE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN((pin->'report'->>'artifact_id')::uuid,(pin->'calibration'->>'artifact_id')::uuid) ORDER BY id FOR SHARE;
 IF NOT photo.v27_semantic_frozen_qualification_ready(p_tenant,p_run)
 OR NOT photo.worker_authorized(p_tenant,p_worker,'gpu',slot.node_id,t.operation,'cuda',t.execution_profile->>'model_lock_id')
 OR NOT photo.v2_input_closure_ready(p_task)
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND (expires_at IS NULL OR expires_at>clock_timestamp()))
 OR t.lease_until<=clock_timestamp() OR t.deadline<=clock_timestamp() OR slot.lease_until<=clock_timestamp() THEN RETURN NULL;END IF;
 RETURN pin;
END $$;
ALTER FUNCTION photo.v27_semantic_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
