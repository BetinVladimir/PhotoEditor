-- +goose Up
-- Only exact staged donor inventory may publish its single chunk and manifest.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_donor_transport_scope(p_tenant uuid,p_run uuid,p_task uuid,p_purpose text)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;plan jsonb;task jsonb;body jsonb;pin jsonb;c jsonb;project_id uuid;previous_tenant text;allowed boolean;
BEGIN
 IF p_purpose IS NULL OR p_purpose NOT IN('v2_input_chunk_0','v2_input_manifest') THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND kind='v2'
 AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR r.v28_donor_descriptor_jcs IS NULL THEN RETURN false;END IF;
 plan:=r.v28_donor_descriptor_jcs::jsonb;task:=plan->'task';body:=plan->'params';pin:=task->'execution_profile'->'donor_qualification';
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 IF NOT FOUND OR t.operation IS DISTINCT FROM 'portrait.open_eyes' OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu' OR t.state IS DISTINCT FROM 'blocked' OR t.attempt IS DISTINCT FROM 0 OR t.cancel_requested IS DISTINCT FROM false
 OR task->>'id' IS DISTINCT FROM t.id::text OR task->>'node_key' IS DISTINCT FROM t.node_key
 OR task->>'params_artifact_id' IS DISTINCT FROM t.params_artifact_id::text OR task->>'input_digest' IS DISTINCT FROM t.input_digest
 OR (t.execution_profile IS DISTINCT FROM task->'execution_profile' AND NOT(photo.v2_input_closure_ready(t.id)
 AND (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=task->'execution_profile')) THEN RETURN false;END IF;
 SELECT w.project_id INTO project_id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run
 AND w.input_snapshot_id=r.input_snapshot_id AND w.workflow_type='preview' AND w.renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT d.v28_donor_issuer_context INTO c FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run
 AND d.artifact_id=(body->'confirmation'->>'artifact_id')::uuid AND d.sha256=body->'confirmation'->>'sha256'
 AND d.purpose='v28_donor_confirmation' AND d.state='ready' FOR SHARE;
 IF NOT FOUND OR c->>'actor_id' IS DISTINCT FROM r.created_by::text OR c->>'project_id' IS DISTINCT FROM project_id::text
 OR (c->'params'-'confirmation') IS DISTINCT FROM (body-'confirmation') THEN RETURN false;END IF;
 IF photo.v28_donor_inventory_digest_ready(t.id,t.params_artifact_id,task->>'params_sha',t.operation,t.input_digest,plan->'entries') IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM d.artifact_id FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.purpose='task_params' AND d.state='ready' AND d.artifact_id=t.params_artifact_id
 AND d.sha256=task->>'params_sha' AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version=d.object_version AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF d,a;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run
 AND state='reserved' AND expires_at>clock_timestamp() AND bytes::numeric>=(plan->>'reserved_bytes')::numeric+65536+2097152 FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 previous_tenant:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 allowed:=photo.v28_donor_confirmation_scope(p_tenant,project_id,(body->'base'->>'asset_version_id')::uuid,
 (body->'confirmation'->>'artifact_id')::uuid,body->'confirmation'->>'sha256',r.created_by,(c->>'auth_session_id')::uuid,
 (body->'base'->'source'->>'artifact_id')::uuid,body->'base'->'source'->>'sha256') IS TRUE
 AND photo.lock_v28_donor_qualification_objects(p_tenant,project_id,r.created_by,(c->>'auth_session_id')::uuid,pin,plan->'qualification_objects') IS TRUE;
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 RETURN allowed;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_donor_transport_scope(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_donor_transport_scope(uuid,uuid,uuid,text) FROM PUBLIC;
ALTER FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) RENAME TO v2_input_document_scope_pre_v28_donor;
CREATE FUNCTION photo.v2_input_document_scope(p_tenant uuid,p_run uuid,p_task uuid,p_purpose text)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND v28_donor_descriptor_jcs IS NOT NULL)
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task AND operation='portrait.open_eyes') THEN
 RETURN photo.v28_donor_transport_scope(p_tenant,p_run,p_task,p_purpose);END IF;
 RETURN photo.v2_input_document_scope_pre_v28_donor(p_tenant,p_run,p_task,p_purpose);
END $$;
ALTER FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
