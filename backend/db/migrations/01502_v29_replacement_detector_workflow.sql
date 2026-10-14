-- +goose Up
-- Draft execution pins only; source IO verification remains required before seal.
ALTER TABLE photo.runs ADD COLUMN v29_replacement_detector_execution_policy text;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v29_replacement_detector_execution_policy_scope CHECK (
 v29_replacement_detector_execution_policy IS NULL OR
 ((config_snapshot ? 'v29_perspective_replacement_detection_jcs') AND octet_length(v29_replacement_detector_execution_policy) BETWEEN 2 AND 65536));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_replacement_detector_execution_policy() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v29_replacement_detector_execution_policy IS NOT NULL THEN
   RAISE EXCEPTION 'Execution policy must be bound after source verification' USING ERRCODE='42501';
  END IF;
 ELSIF NEW.v29_replacement_detector_execution_policy IS DISTINCT FROM OLD.v29_replacement_detector_execution_policy THEN
  IF current_user<>'photo_fn_owner' OR OLD.v29_replacement_detector_execution_policy IS NOT NULL
   OR NOT (OLD.config_snapshot ? 'v29_perspective_replacement_detection_jcs') OR OLD.status<>'draft' OR OLD.graph_sealed OR OLD.cancel_requested
   OR NEW.status IS DISTINCT FROM OLD.status OR NEW.graph_sealed IS DISTINCT FROM OLD.graph_sealed
   OR NEW.cancel_requested IS DISTINCT FROM OLD.cancel_requested THEN
   RAISE EXCEPTION 'Execution policy is immutable' USING ERRCODE='42501';
  END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_replacement_detector_execution_policy() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_replacement_detector_execution_policy() FROM PUBLIC;
CREATE TRIGGER runs_v29_replacement_detector_execution_policy_guard BEFORE INSERT OR UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_replacement_detector_execution_policy();

CREATE FUNCTION photo.create_v29_replacement_detector_workflow(p_tenant uuid,p_run uuid,p_policy text,p_proof jsonb)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;policy jsonb;plan jsonb;
 policy_sha text;workflow uuid;h bytea;project uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_policy IS NULL OR octet_length(p_policy) NOT BETWEEN 2 AND 65536
 OR photo.read_v29_replacement_detector_transport_admission(p_tenant,p_run,((p_proof->>'descriptor_jcs')::jsonb->>'task_id')::uuid,p_proof) IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.failure_policy<>'fail_fast' THEN RETURN NULL;END IF;
 plan:=(r.config_snapshot->>'v29_perspective_replacement_detection_jcs')::jsonb;project:=(plan->>'project_id')::uuid;
 policy:=p_policy::jsonb;
 IF coalesce(policy->>'runtime_lock_sha','') !~ '^[0-9a-f]{64}$'
 OR policy IS DISTINCT FROM jsonb_build_object('schema_version',1,'operation','optical.perspective_lines',
 'operation_version',1,'backend','cpu','precision','float32','renderer_version','v2.2',
 'working_icc_sha',((plan->>'source_jcs')::jsonb->'color'->>'icc_sha256'),'runtime_lock_sha',policy->>'runtime_lock_sha',
 'max_pixels',200000000,'proxy_edge',1600,'max_lines',128) THEN RETURN NULL;END IF;
 policy_sha:=encode(sha256(convert_to(p_policy,'UTF8')),'hex');
 h:=public.digest(uuid_send(p_run)||convert_to('workflow','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 workflow:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF FOUND THEN
  IF r.v29_replacement_detector_execution_policy IS NOT DISTINCT FROM p_policy
   AND w.id=workflow AND w.project_id=project AND w.input_snapshot_id=r.input_snapshot_id
   AND w.workflow_type='preview' AND w.renderer_version='v2.2' AND w.policy_sha=policy_sha THEN RETURN workflow;END IF;
  RETURN NULL;
 END IF;
 IF r.v29_replacement_detector_execution_policy IS NOT NULL THEN RETURN NULL;END IF;
 UPDATE photo.runs SET v29_replacement_detector_execution_policy=p_policy WHERE tenant_id=p_tenant AND id=p_run;
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(workflow,p_tenant,project,p_run,r.input_snapshot_id,'preview','v2.2',policy_sha);
 RETURN workflow;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.create_v29_replacement_detector_workflow(uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v29_replacement_detector_workflow(uuid,uuid,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.create_v29_replacement_detector_workflow(uuid,uuid,text,jsonb) TO photo_scheduler;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
