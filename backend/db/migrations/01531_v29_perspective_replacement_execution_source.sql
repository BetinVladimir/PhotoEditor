-- +goose Up
-- Private source authority for leased execution and completed-task replay.
-- Object-byte, qualification, closure, lease and result gates remain independent.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_execution_session(p_config jsonb)
RETURNS uuid LANGUAGE sql IMMUTABLE SET search_path=pg_catalog AS $$
 SELECT CASE WHEN p_config ? 'v29_negative_replacement_draft_jcs'
 THEN (((p_config->>'v29_negative_replacement_draft_jcs')::jsonb)->'draft'->>'auth_session_id')::uuid
 ELSE (p_config->'v29_perspective'->>'auth_session_id')::uuid END;
$$;
ALTER FUNCTION photo.v29_perspective_execution_session(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_execution_session(jsonb) FROM PUBLIC;

CREATE FUNCTION photo.v29_perspective_execution_source_scope(p_tenant uuid,p_project uuid,p_snapshot uuid,
 p_actor uuid,p_session uuid,p_task uuid,p_base jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;plan jsonb;source jsonb;previous_tenant text;answer boolean;
BEGIN
 previous_tenant:=current_setting('app.tenant_id',true);
 IF previous_tenant IS NOT NULL AND previous_tenant<>'' AND previous_tenant<>p_tenant::text THEN RETURN false;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 SELECT run.* INTO r FROM photo.tasks task JOIN photo.runs run ON run.tenant_id=task.tenant_id AND run.id=task.run_id
 WHERE task.tenant_id=p_tenant AND task.id=p_task AND task.operation='optical.perspective' AND task.operation_version='1';
 IF r.id IS NULL OR r.input_snapshot_id IS DISTINCT FROM p_snapshot OR r.created_by IS DISTINCT FROM p_actor THEN
 answer:=false;
 ELSIF r.config_snapshot ? 'v29_negative_replacement_draft_jcs' THEN
 plan:=photo.v29_perspective_execution_plan(p_tenant,r.id);
 source:=photo.v29_perspective_replacement_source(p_tenant,p_project,p_actor,p_session,p_task);
 answer:=plan IS NOT NULL AND source IS NOT NULL
 AND plan->>'task_id'=p_task::text AND plan->>'project_id'=p_project::text
 AND plan->>'auth_session_id'=p_session::text
 AND ((plan->>'params_jcs')::jsonb)->'base'=p_base
 AND source->>'artifact_id'=p_base->'source'->>'artifact_id'
 AND source->>'sha256'=p_base->'source'->>'sha256';
 ELSE
 answer:=photo.v28_local_portrait_source_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base);
 END IF;
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 RETURN coalesce(answer,false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);RETURN false;
END $$;
ALTER FUNCTION photo.v29_perspective_execution_source_scope(uuid,uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_execution_source_scope(uuid,uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
DECLARE signature text;definition text;old_session text;old_scope text;
BEGIN
 FOREACH signature IN ARRAY ARRAY[
 'photo.v29_worker_perspective_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
 'photo.v29_worker_perspective_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
 'photo.v29_perspective_adoption_evidence(uuid,uuid,integer,bigint,uuid,text)'] LOOP
 definition:=pg_get_functiondef(signature::regprocedure);
 old_session:='(r.config_snapshot->''v29_perspective''->>''auth_session_id'')::uuid';
 IF strpos(definition,old_session)>0 THEN
 definition:=replace(definition,old_session,'photo.v29_perspective_execution_session(r.config_snapshot)');
 ELSE
 old_session:='r.config_snapshot->''v29_perspective''->>''auth_session_id''';
 IF strpos(definition,old_session)=0 THEN RAISE EXCEPTION 'PERSPECTIVE_SESSION_BASELINE_MISMATCH: %',signature;END IF;
 definition:=replace(definition,old_session,'photo.v29_perspective_execution_session(r.config_snapshot)::text');
 END IF;
 IF signature LIKE 'photo.v29_worker_perspective_source_ready(%' THEN
 old_scope:='photo.v28_local_portrait_source_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,auth_session,body->''base'')';
 IF strpos(definition,old_scope)=0 THEN RAISE EXCEPTION 'PERSPECTIVE_WORKER_SOURCE_BASELINE_MISMATCH';END IF;
 definition:=replace(definition,old_scope,'photo.v29_perspective_execution_source_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,auth_session,p_task,body->''base'')');
 ELSIF signature LIKE 'photo.v29_perspective_adoption_evidence(%' THEN
 old_scope:='photo.v28_local_portrait_source_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,';
 IF strpos(definition,old_scope)=0 THEN RAISE EXCEPTION 'PERSPECTIVE_ADOPTION_SOURCE_BASELINE_MISMATCH';END IF;
 definition:=replace(definition,old_scope,'photo.v29_perspective_execution_source_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,');
 old_scope:='photo.v29_perspective_execution_session(r.config_snapshot),body->''base'')';
 IF strpos(definition,old_scope)=0 THEN RAISE EXCEPTION 'PERSPECTIVE_ADOPTION_TASK_BASELINE_MISMATCH';END IF;
 definition:=replace(definition,old_scope,'photo.v29_perspective_execution_session(r.config_snapshot),p_task,body->''base'')');
 END IF;
 EXECUTE definition;
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
