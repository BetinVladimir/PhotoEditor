-- +goose Up
-- Durable recovery data only: no tasks, transport, outbox or activation.
ALTER TABLE photo.runs ADD COLUMN v28_donor_descriptor_jcs text
 CHECK(v28_donor_descriptor_jcs IS NULL OR octet_length(v28_donor_descriptor_jcs) BETWEEN 2 AND 1048576);
-- +goose StatementBegin
CREATE FUNCTION photo.save_v28_donor_descriptor(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_descriptor text,p_params text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;plan jsonb;task jsonb;body jsonb;pin jsonb;project_id uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_descriptor IS NULL OR octet_length(p_descriptor) NOT BETWEEN 2 AND 1048576
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 plan:=p_descriptor::jsonb;task:=plan->'task';body:=p_params::jsonb;pin:=task->'execution_profile'->'donor_qualification';
 IF jsonb_typeof(plan) IS DISTINCT FROM 'object' OR plan->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(plan) k) IS DISTINCT FROM ARRAY['entries','params','qualification_objects','reserved_bytes','schema_version','task']::text[]
 OR plan->'params' IS DISTINCT FROM body OR task->>'operation' IS DISTINCT FROM 'portrait.open_eyes'
 OR task->>'operation_version' IS DISTINCT FROM '1' OR task->>'resource_class' IS DISTINCT FROM 'cpu'
 OR task->'execution_profile'->>'backend' IS DISTINCT FROM 'cpu' OR task->'execution_profile'->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR task->'execution_profile'->>'portrait_runtime_sha' IS DISTINCT FROM pin->>'runtime_lock_sha'
 OR encode(sha256(convert_to(p_params,'UTF8')),'hex') IS DISTINCT FROM task->>'params_sha'
 OR (plan->>'reserved_bytes')::numeric NOT BETWEEN 1 AND 9007199254740991 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor THEN RETURN false;END IF;
 SELECT w.project_id INTO project_id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run
 AND w.workflow_type='preview' AND w.renderer_version='v2.2' AND w.input_snapshot_id=r.input_snapshot_id FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF photo.v28_donor_confirmation_scope(p_tenant,project_id,(body->'base'->>'asset_version_id')::uuid,
 (body->'confirmation'->>'artifact_id')::uuid,body->'confirmation'->>'sha256',p_actor,p_session,
 (body->'base'->'source'->>'artifact_id')::uuid,body->'base'->'source'->>'sha256') IS DISTINCT FROM true
 OR photo.lock_v28_donor_qualification_objects(p_tenant,project_id,p_actor,p_session,pin,plan->'qualification_objects') IS DISTINCT FROM true
 OR photo.v28_donor_inventory_digest_ready((task->>'id')::uuid,(task->>'params_artifact_id')::uuid,task->>'params_sha',task->>'operation',task->>'input_digest',plan->'entries') IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run
 AND d.artifact_id=(body->'confirmation'->>'artifact_id')::uuid AND d.sha256=body->'confirmation'->>'sha256'
 AND (d.v28_donor_issuer_context->'params'-'confirmation')=(body-'confirmation')) THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM (VALUES('Report'::text,pin->'report'),('Calibration'::text,pin->'calibration'),
 ('DonorConfirmation'::text,body->'confirmation'),('FaceObservations'::text,body->'source_faces'),('FaceObservations'::text,body->'donor_faces'),
 ('WorkingImage'::text,body->'base'->'source'),('WorkingImage'::text,body->'donor'->'source'),('MaskRef'::text,body->'eyes_mask'),('MaskRef'::text,body->'protected')) refs(contract,ref)
 WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(plan->'entries') e WHERE e->>'contract'=refs.contract AND e->'ref'=refs.ref)) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=p_run
 AND q.state='reserved' AND q.expires_at>clock_timestamp() AND q.bytes::numeric>=(plan->>'reserved_bytes')::numeric+65536+2097152) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.purpose='task_params' AND d.state='ready'
 AND d.artifact_id=(task->>'params_artifact_id')::uuid AND d.sha256=task->>'params_sha' AND d.bytes=octet_length(p_params)
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket
 AND a.object_version=d.object_version AND a.object_version NOT IN('','null') AND a.media_type='application/json'
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 IF r.v28_donor_descriptor_jcs IS NOT NULL THEN RETURN r.v28_donor_descriptor_jcs=p_descriptor;END IF;
 UPDATE photo.runs SET v28_donor_descriptor_jcs=p_descriptor WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v28_donor_descriptor(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v28_donor_descriptor(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v28_donor_descriptor(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_donor_descriptor(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;body jsonb;project_id uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND created_by=p_actor
 AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR r.v28_donor_descriptor_jcs IS NULL THEN RETURN NULL;END IF;
 body:=r.v28_donor_descriptor_jcs::jsonb->'params';
 SELECT w.project_id INTO project_id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run
 AND w.workflow_type='preview' AND w.renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR photo.v28_donor_confirmation_scope(p_tenant,project_id,(body->'base'->>'asset_version_id')::uuid,
 (body->'confirmation'->>'artifact_id')::uuid,body->'confirmation'->>'sha256',p_actor,p_session,
 (body->'base'->'source'->>'artifact_id')::uuid,body->'base'->'source'->>'sha256') IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN r.v28_donor_descriptor_jcs;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v28_donor_descriptor(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_donor_descriptor(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_donor_descriptor(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
