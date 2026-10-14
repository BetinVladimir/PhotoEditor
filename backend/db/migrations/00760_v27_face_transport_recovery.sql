-- +goose Up
-- Recover exact registered transport for SQL-only seal retry after unknown commit.
-- No repair, publication, activation or receipt shortcut.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_face_registered_transport(p_tenant uuid,p_run uuid,p_task uuid,p_actor uuid,p_session uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;project uuid;params_sha text;rows jsonb;chunks jsonb;binding jsonb;state text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 SELECT project_id INTO project FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF r.id IS NULL OR t.id IS NULL OR r.created_by IS DISTINCT FROM p_actor OR t.operation NOT IN ('face.landmarks','face.parse')
 OR photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) IS NOT TRUE
 OR r.config_snapshot->'v27_face_prepare'->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM project::text
 OR jsonb_typeof(t.execution_profile->'v2_input_manifest') IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
 SELECT a.sha256 INTO params_sha FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id AND a.state='ready';
 SELECT coalesce(jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),'contract',i.contract) ORDER BY i.ordinal),'[]'::jsonb) INTO rows
 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal<1000000;
 SELECT coalesce(jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256) ORDER BY i.ordinal),'[]'::jsonb) INTO chunks
 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal>=1000000 AND i.contract='InputClosureChunk';
 binding:=jsonb_build_object('schema_version',1,'task_id',t.id,'params',jsonb_build_object('artifact_id',t.params_artifact_id,'sha256',params_sha),'input_digest',t.input_digest,
 'profile',t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha',
 'transport',jsonb_build_object('schema_version',1,'rows',rows,'manifest',t.execution_profile->'v2_input_manifest','chunks',chunks,'direct',t.inputs-0,'count',(t.execution_profile->>'v2_entry_count')::integer,'entries_sha',t.execution_profile->>'v2_entries_sha'));
 IF r.graph_sealed THEN
  state:=photo.semantic_face_completed_binding_state(p_tenant,p_run,t.operation,binding);
 ELSE
  state:=photo.semantic_face_registration_binding_state(p_tenant,p_run,t.operation,binding);
 END IF;
 IF state IS DISTINCT FROM 'replayed' THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,'task_id',p_task,'manifest',binding->'transport'->'manifest','chunks',chunks,'count',binding->'transport'->'count','entries_sha',binding->'transport'->'entries_sha');
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.semantic_face_registered_transport(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_face_registered_transport(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_face_registered_transport(uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
