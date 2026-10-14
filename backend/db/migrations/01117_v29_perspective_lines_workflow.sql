-- +goose Up
-- Execution pins are deployment configuration, not quality qualification.
ALTER TABLE photo.runs ADD COLUMN v29_perspective_lines_execution_policy text;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v29_lines_execution_policy_scope CHECK (
 v29_perspective_lines_execution_policy IS NULL OR
 (v29_perspective_line_detection AND octet_length(v29_perspective_lines_execution_policy) BETWEEN 2 AND 65536));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_perspective_lines_execution_policy() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v29_perspective_lines_execution_policy IS NOT NULL THEN
   RAISE EXCEPTION 'Execution policy must be bound after source verification' USING ERRCODE='42501';
  END IF;
 ELSIF NEW.v29_perspective_lines_execution_policy IS DISTINCT FROM OLD.v29_perspective_lines_execution_policy THEN
  IF current_user<>'photo_fn_owner' OR OLD.v29_perspective_lines_execution_policy IS NOT NULL
   OR NOT OLD.v29_perspective_line_detection OR OLD.status<>'draft' OR OLD.graph_sealed OR OLD.cancel_requested
   OR NEW.status IS DISTINCT FROM OLD.status OR NEW.graph_sealed IS DISTINCT FROM OLD.graph_sealed
   OR NEW.cancel_requested IS DISTINCT FROM OLD.cancel_requested THEN
   RAISE EXCEPTION 'Execution policy is immutable' USING ERRCODE='42501';
  END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_perspective_lines_execution_policy() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_perspective_lines_execution_policy() FROM PUBLIC;
CREATE TRIGGER runs_v29_lines_execution_policy_guard BEFORE INSERT OR UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_perspective_lines_execution_policy();

CREATE FUNCTION photo.create_v29_perspective_lines_workflow(p_tenant uuid,p_run uuid,p_policy text,p_witness jsonb)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;policy jsonb;plan jsonb;
 policy_sha text;workflow uuid;h bytea;project uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_policy IS NULL OR octet_length(p_policy) NOT BETWEEN 2 AND 65536
 OR photo.v29_perspective_lines_inputs_ready(p_tenant,p_run,p_witness) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.failure_policy<>'fail_fast' THEN RETURN NULL;END IF;
 plan:=r.config_snapshot->'v29_perspective_line_detection';project:=(plan->>'project_id')::uuid;
 policy:=p_policy::jsonb;
 IF coalesce(policy->>'runtime_lock_sha','') !~ '^[0-9a-f]{64}$'
 OR policy IS DISTINCT FROM jsonb_build_object('schema_version',1,'operation','optical.perspective_lines',
 'operation_version',1,'backend','cpu','precision','float32','renderer_version','v2.2',
 'working_icc_sha',plan->>'working_icc_sha','runtime_lock_sha',policy->>'runtime_lock_sha',
 'max_pixels',200000000,'proxy_edge',1600,'max_lines',128) THEN RETURN NULL;END IF;
 policy_sha:=encode(sha256(convert_to(p_policy,'UTF8')),'hex');
 h:=public.digest(uuid_send(p_run)||convert_to('workflow','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 workflow:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF FOUND THEN
  IF r.v29_perspective_lines_execution_policy IS NOT DISTINCT FROM p_policy
   AND w.id=workflow AND w.project_id=project AND w.input_snapshot_id=r.input_snapshot_id
   AND w.workflow_type='preview' AND w.renderer_version='v2.2' AND w.policy_sha=policy_sha THEN RETURN workflow;END IF;
  RETURN NULL;
 END IF;
 IF r.v29_perspective_lines_execution_policy IS NOT NULL THEN RETURN NULL;END IF;
 UPDATE photo.runs SET v29_perspective_lines_execution_policy=p_policy WHERE tenant_id=p_tenant AND id=p_run;
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(workflow,p_tenant,project,p_run,r.input_snapshot_id,'preview','v2.2',policy_sha);
 RETURN workflow;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.create_v29_perspective_lines_workflow(uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v29_perspective_lines_workflow(uuid,uuid,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.create_v29_perspective_lines_workflow(uuid,uuid,text,jsonb) TO photo_scheduler;

-- Registration must bind the persisted policy, not any generic preview workflow.
CREATE OR REPLACE FUNCTION photo.register_v29_perspective_lines_inputs(p_tenant uuid,p_run uuid,p_proof jsonb,p_witness jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;c photo.backend_documents%ROWTYPE;m photo.backend_documents%ROWTYPE;
 plan jsonb;image jsonb;chunk jsonb;manifest jsonb;expected jsonb;actual jsonb;row jsonb;role text;ref jsonb;
 project uuid;task uuid;count_entries integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_proof IS NULL OR jsonb_typeof(p_proof) IS DISTINCT FROM 'object' OR pg_column_size(p_proof)>262144
 OR photo.v29_perspective_lines_inputs_ready(p_tenant,p_run,p_witness) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 plan:=r.config_snapshot->'v29_perspective_line_detection';project:=(plan->>'project_id')::uuid;task:=(plan->>'task_id')::uuid;
 image:=(plan->>'source_jcs')::jsonb;
 PERFORM id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND project_id=project
 AND input_snapshot_id=r.input_snapshot_id AND workflow_type='preview' AND renderer_version='v2.2'
 AND r.v29_perspective_lines_execution_policy IS NOT NULL
 AND policy_sha=encode(sha256(convert_to(r.v29_perspective_lines_execution_policy,'UTF8')),'hex') FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=task FOR UPDATE;
 IF NOT FOUND OR t.state<>'blocked' OR t.cancel_requested THEN RETURN false;END IF;
 SELECT * INTO c FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=task AND purpose='v2_input_chunk_0' AND state='ready' FOR UPDATE;
 IF NOT FOUND OR c.v29_perspective_lines_transport IS NULL THEN RETURN false;END IF;
 SELECT * INTO m FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=task AND purpose='v2_input_manifest' AND state='ready' FOR UPDATE;
 IF NOT FOUND OR m.v29_perspective_lines_transport IS NULL THEN RETURN false;END IF;
 -- Current input gate independently reconstructed both control bodies and
 -- verified their ready artifact locators before accepting IO witnesses.
 chunk:=c.v29_perspective_lines_transport::jsonb;manifest:=m.v29_perspective_lines_transport::jsonb;
 count_entries:=jsonb_array_length(chunk->'entries');
 IF p_proof IS DISTINCT FROM jsonb_build_object('schema_version',1,'task_id',task,
 'entries',chunk->'entries','entry_count',count_entries,'entries_sha',manifest->>'entries_sha',
 'chunks',jsonb_build_array(jsonb_build_object('artifact_id',c.artifact_id,'sha256',c.sha256)),
 'manifest',jsonb_build_object('artifact_id',m.artifact_id,'sha256',m.sha256),
 'chunk_jcs',c.v29_perspective_lines_transport,'manifest_jcs',m.v29_perspective_lines_transport) THEN RETURN false;END IF;
 -- Every historical dense occurrence must still be an authorized source child,
 -- even though the detector reads only current RGB/alpha pixels.
 FOR role,ref IN SELECT x.role,x.ref FROM (
  SELECT 'WorkingPixels'::text role,image->'pixels' ref
  UNION ALL SELECT 'WorkingAlpha',image->'alpha' WHERE image->'alpha'<>'null'::jsonb
  UNION ALL SELECT 'InverseField/'||(n-1)::text,v->'inverse_field'
   FROM jsonb_array_elements(image->'transform_chain'->'steps') WITH ORDINALITY a(v,n) WHERE v->>'kind'='dense_warp'
 ) x LOOP
  IF photo.lock_v29_perspective_source_child(p_tenant,project,r.input_snapshot_id,r.created_by,
   (plan->>'auth_session_id')::uuid,plan->'base',plan->>'source_jcs',(ref->>'artifact_id')::uuid,ref->>'sha256',role) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 expected:=chunk->'entries'||jsonb_build_array(
 jsonb_build_object('contract','InputClosureChunk','ref',jsonb_build_object('artifact_id',c.artifact_id,'sha256',c.sha256)),
 jsonb_build_object('contract','InputClosureManifest','ref',jsonb_build_object('artifact_id',m.artifact_id,'sha256',m.sha256)));
 IF (SELECT count(DISTINCT v->'ref'->>'artifact_id') FROM jsonb_array_elements(expected) v)<>jsonb_array_length(expected) THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('contract',i.contract,'ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256)) ORDER BY i.ordinal)
 INTO actual FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=task;
 IF actual IS NOT NULL THEN
  RETURN actual=expected AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=task
  AND (i.project_id<>project OR i.ordinal NOT BETWEEN 0 AND count_entries-1 AND i.ordinal NOT IN(1000000,1000001)));
 END IF;
 -- All checks precede mutation. A constraint error aborts the caller's
 -- transaction; no partial successful registration is returned.
 INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal)
 SELECT p_tenant,project,task,(v->'ref'->>'artifact_id')::uuid,v->'ref'->>'sha256',v->>'contract',
 CASE WHEN n<=count_entries THEN (n-1)::integer ELSE 1000000+(n-count_entries-1)::integer END
 FROM jsonb_array_elements(expected) WITH ORDINALITY a(v,n);
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.register_v29_perspective_lines_inputs(uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v29_perspective_lines_inputs(uuid,uuid,jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.register_v29_perspective_lines_inputs(uuid,uuid,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
