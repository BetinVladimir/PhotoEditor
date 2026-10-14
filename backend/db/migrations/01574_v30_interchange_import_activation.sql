-- +goose Up
-- Dedicated continuation activation. The scheduler must first physically
-- qualify the exact closure and independently measured execution profile.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v30_interchange_import(p_tenant uuid,p_workflow uuid,p_parsed text,p_plan text,p_params text,p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;
 capacity numeric;used numeric;largest bigint;reserved bigint;estimate jsonb;params jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow AND workflow_type='interchange' AND renderer_version='v2.2';
 -- Scope locks the existing tenant/run/task before materialization replay, so
 -- an activation refusal cannot create a shell or increase a reservation.
 IF w.id IS NULL OR photo.v30_interchange_transport_scope(p_tenant,w.run_id,photo.v30_uuid5(w.id,'v2.interchange.import:0'),p_parsed) IS DISTINCT FROM w.id THEN RETURN false;END IF;
 IF photo.materialize_v30_interchange_import(p_tenant,p_workflow,p_parsed,p_plan,p_params,p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND id=photo.v30_uuid5(w.id,'v2.interchange.import:0') FOR UPDATE;
 IF t.id IS NULL OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested OR t.terminal_reason IS NOT NULL
 OR r.status<>'running' OR NOT r.graph_sealed OR r.cancel_requested
 OR t.input_digest IS DISTINCT FROM p_task->>'input_digest'
 OR t.execution_profile->>'v2_entries_sha' IS DISTINCT FROM t.input_digest
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 params:=p_params::jsonb;
 IF t.inputs IS DISTINCT FROM jsonb_build_array(t.execution_profile->'v2_input_manifest')||jsonb_build_array(
 jsonb_build_object('artifact_id',t.params_artifact_id,'sha256',p_task->>'params_sha'),params->'plan',params->'parsed',params->'evidence') THEN RETURN false;END IF;
 -- Lock every current immutable object, including transport artifacts, before
 -- checking the full registered closure rather than just direct JSON roots.
 PERFORM a.id FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
 WHERE i.tenant_id=p_tenant AND i.task_id=t.id ORDER BY a.id FOR SHARE OF a;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL
 OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN false;END IF;
 estimate:=t.execution_profile->'resource_estimate';reserved:=(estimate->>'output_bytes')::bigint;
 SELECT coalesce(max(a.bytes),0) INTO largest FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
 WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.contract IN('WorkingPixels','WorkingAlpha','MaskArray','DenseWarpArray','DenseWarpField','LUTArray');
 IF coalesce((estimate->>'scratch_bytes')::bigint,0)<greatest(largest,8388608)
 OR NOT EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=r.id
 AND state='reserved' AND bytes>=reserved AND expires_at>clock_timestamp()) THEN RETURN false;END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active';
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF capacity IS NULL OR used IS NULL OR used>capacity THEN RETURN false;END IF;
 -- Existing CAS repeats live graph/dependency/direct artifact gates and changes
 -- only this node. Parent remains sealed and running until adoption/review.
 RETURN photo.activate_bound_task(t.id,t.input_digest,t.params_artifact_id);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.activate_v30_interchange_import(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_interchange_import(uuid,uuid,text,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.activate_v30_interchange_import(uuid,uuid,text,text,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
