-- +goose Up
-- Scope admission only; verified pixel loading and renderer qualification remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.admit_semantic_local_exposure(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;s photo.resource_slots%ROWTYPE;e jsonb;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 OR p_sha IS NULL
 OR p_sha !~ '^[0-9a-f]{64}$' OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex')<>p_sha
 OR p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF r.id IS NULL OR r.kind<>'apply' OR r.cancel_requested OR r.status NOT IN('queued','running') THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 IF t.id IS NULL OR t.operation<>'semantic.local_exposure' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.state<>'running' OR t.cancel_requested OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.worker_id IS DISTINCT FROM p_worker OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
 OR t.deadline IS NULL OR t.deadline<=clock_timestamp() OR t.params_artifact_id IS DISTINCT FROM p_params
 OR t.input_digest IS DISTINCT FROM p_digest OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task FOR SHARE;
 IF s.id IS NULL OR s.fencing_token IS DISTINCT FROM p_fence OR s.resource_class IS DISTINCT FROM 'cpu'
 OR s.lease_until IS NULL OR s.lease_until<=clock_timestamp()
 OR NOT photo.worker_authorized(p_tenant,p_worker,'cpu',s.node_id,t.operation,'cpu',t.execution_profile->>'model_lock_id') THEN RETURN false;END IF;
 PERFORM worker_id FROM photo.worker_registrations WHERE tenant_id=p_tenant AND worker_id=p_worker
 AND db_principal=session_user AND enabled AND v2_renderer_versions ? 'v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND sha256=p_sha
 AND state='ready' AND verified_at IS NOT NULL AND media_type='application/json' AND bytes=octet_length(p_body)
 AND object_version IS NOT NULL AND object_version NOT IN('','null') AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 e:=p_body::jsonb;
 IF e->>'kind' IS DISTINCT FROM 'local_exposure' OR e->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR e->'preserve_persons' IS DISTINCT FROM 'true'::jsonb
 OR jsonb_typeof(r.config_snapshot->'approved_semantic'->'nodes') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF (SELECT count(*) FROM jsonb_array_elements(r.config_snapshot->'approved_semantic'->'nodes') n
 WHERE n->>'task_id'=p_task::text AND n->>'params_artifact_id'=p_params::text AND n->>'params_sha'=p_sha
 AND n->>'target_id'=e->>'target_id')<>1 THEN RETURN false;END IF;
 RETURN photo.semantic_approved_child_edit(p_tenant,p_run,(e->>'target_id')::uuid,
 (e->'source'->>'asset_version_id')::uuid,(e->'candidate'->>'artifact_id')::uuid,e->'candidate'->>'sha256',
 (e->'source'->'source'->>'artifact_id')::uuid,e->'source'->'source'->>'sha256',
 (e->'intent'->>'artifact_id')::uuid,e->'intent'->>'sha256')
 AND t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp() AND s.lease_until>clock_timestamp();
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.admit_semantic_local_exposure(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.admit_semantic_local_exposure(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.admit_semantic_local_exposure(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
