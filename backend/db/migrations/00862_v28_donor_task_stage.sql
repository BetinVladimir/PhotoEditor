-- +goose Up
-- Register only a blocked donor task; activation remains an independent gate.
-- +goose StatementBegin
CREATE FUNCTION photo.stage_v28_donor_task(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_descriptor text,p_params text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE plan jsonb;task jsonb;old photo.tasks%ROWTYPE;workflow_id uuid;task_id uuid;h bytea;
BEGIN
 IF p_descriptor IS NULL OR octet_length(p_descriptor) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 plan:=p_descriptor::jsonb;task:=plan->'task';
 IF jsonb_typeof(task) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(task) k) IS DISTINCT FROM ARRAY['execution_profile','id','input_digest','node_key','operation','operation_version','params_artifact_id','params_sha','resource_class']::text[]
 OR coalesce(task->>'node_key','') !~ '^portrait[.]open_eyes:(0|[1-9][0-9]{0,3})$'
 OR split_part(task->>'node_key',':',2)::integer>=5000 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND created_by=p_actor
 AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT id INTO workflow_id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run
 AND workflow_type='preview' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 h:=public.digest(uuid_send(workflow_id)||convert_to(task->>'node_key','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 task_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF task_id::text IS DISTINCT FROM task->>'id' THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=task_id FOR UPDATE;
 IF FOUND AND (old.run_id IS DISTINCT FROM p_run OR old.node_key IS DISTINCT FROM task->>'node_key'
 OR old.operation IS DISTINCT FROM 'portrait.open_eyes' OR old.operation_version IS DISTINCT FROM '1' OR old.resource_class IS DISTINCT FROM 'cpu'
 OR old.params_artifact_id::text IS DISTINCT FROM task->>'params_artifact_id' OR old.input_digest IS DISTINCT FROM task->>'input_digest'
 OR old.state IS DISTINCT FROM 'blocked' OR old.attempt IS DISTINCT FROM 0 OR old.cancel_requested IS DISTINCT FROM false
 OR (old.execution_profile IS DISTINCT FROM task->'execution_profile' AND NOT(photo.v2_input_closure_ready(old.id)
 AND (old.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=task->'execution_profile'))) THEN RETURN false;END IF;
 IF photo.save_v28_donor_descriptor(p_tenant,p_run,p_actor,p_session,p_descriptor,p_params) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF old.id IS NOT NULL THEN RETURN true;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(task_id,p_tenant,p_run,task->>'node_key','portrait.open_eyes','1','cpu','blocked','[]'::jsonb,
 (task->>'params_artifact_id')::uuid,task->>'input_digest',task->'execution_profile');
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.stage_v28_donor_task(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v28_donor_task(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v28_donor_task(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
