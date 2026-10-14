-- +goose Up
-- Terminal state only. No output, registration or admission authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_native_initial_terminal_state(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid)
RETURNS TABLE(operation text) LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH anchor AS (
 SELECT r.*,r.config_snapshot->'v27_detector_prepare' AS native
 FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND r.tenant_id=p_tenant AND r.id=photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference')
 AND r.kind='v2' AND r.created_by=p_actor AND r.graph_sealed AND r.v27_initial_recovery_enabled
 AND r.config_snapshot->>'project_id'=p_project::text
 AND r.config_snapshot->'v27_detector_prepare'->'schema_version'='1'::jsonb
 AND r.config_snapshot->'v27_detector_prepare'->>'parent_run_id'=p_parent::text
 AND r.config_snapshot->'v27_detector_prepare'->>'auth_session_id'=p_session::text
 AND r.config_snapshot->'v27_detector_prepare'->>'request_sha' ~ '^[0-9a-f]{64}$'
 AND jsonb_typeof(r.config_snapshot->'v27_detector_prepare'->'source')='object'
 AND t.operation='face.detect' AND t.operation_version='1' AND t.node_key='face.detect:0'
 AND t.id=photo.semantic_uuid_v5(photo.semantic_uuid_v5(r.id,'workflow'),'face.detect:0')
 AND r.config_snapshot->'v27_detector_prepare'->>'task_id'=t.id::text
 AND (SELECT count(*) FROM photo.tasks other WHERE other.tenant_id=p_tenant AND other.run_id=r.id)=1
 ), terminal AS (
 SELECT t.operation
 FROM anchor a JOIN photo.runs stage ON stage.tenant_id=p_tenant AND stage.created_by=p_actor AND stage.kind='v2'
 JOIN photo.tasks t ON t.tenant_id=stage.tenant_id AND t.run_id=stage.id
 CROSS JOIN LATERAL (SELECT stage.config_snapshot->CASE t.operation
  WHEN 'face.detect' THEN 'v27_detector_prepare' WHEN 'mask.person' THEN 'v27_person_prepare'
  WHEN 'mask.face_skin' THEN 'v27_skin_prepare' WHEN 'defects.propose' THEN 'v27_defect_native_prepare'
  ELSE 'v27_face_prepare' END AS native) selected
 WHERE t.operation IN('face.detect','mask.person','face.landmarks','face.parse','mask.face_skin','defects.propose')
 AND (t.operation<>'defects.propose' OR a.v27_initial_manual_selection IS NOT NULL)
 AND t.operation_version='1' AND stage.graph_sealed
 AND stage.id=photo.semantic_uuid_v5(p_parent,CASE WHEN t.operation='defects.propose' THEN 'semantic.defect.prepare:reference' ELSE 'semantic.'||t.operation||':reference' END)
 AND t.node_key=t.operation||':0' AND t.id=photo.semantic_uuid_v5(photo.semantic_uuid_v5(stage.id,'workflow'),t.operation||':0')
 AND (SELECT count(*) FROM photo.tasks other WHERE other.tenant_id=p_tenant AND other.run_id=stage.id)=1
 AND stage.config_snapshot->>'project_id'=p_project::text
 AND selected.native->'schema_version'='1'::jsonb AND selected.native->>'parent_run_id'=p_parent::text
 AND selected.native->>'auth_session_id'=p_session::text AND selected.native->'request_sha'=a.native->'request_sha'
 AND selected.native->'source'=a.native->'source'
 AND selected.native->'intent'=a.native->'intent' AND selected.native->'snapshot'=a.native->'snapshot'
 AND selected.native->>'task_id'=t.id::text
 AND (t.operation IN('face.detect','mask.person') OR selected.native->>'operation'=t.operation)
 AND (t.state IN('failed','cancelled','skipped') OR stage.status IN('failed','cancelled','cancelling'))
 )
 SELECT (SELECT terminal.operation FROM terminal ORDER BY terminal.operation LIMIT 1) FROM anchor;
$$;
ALTER FUNCTION photo.v27_native_initial_terminal_state(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_native_initial_terminal_state(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_native_initial_terminal_state(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.record_v27_native_initial_pipeline(p_tenant uuid,p_detector uuid,p_token uuid,p_outcome text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;parent uuid;terminal boolean:=false;
BEGIN
 IF p_token IS NULL OR p_outcome IS NULL OR p_outcome NOT IN('retry','waiting','completed','failed') THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_detector FOR UPDATE;
 IF NOT FOUND OR r.v27_initial_recovery_token IS DISTINCT FROM p_token OR r.v27_initial_recovery_until<=clock_timestamp() OR r.v27_initial_recovery_until IS NULL THEN RETURN false;END IF;
 parent:=(r.config_snapshot->'v27_detector_prepare'->>'parent_run_id')::uuid;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF p_outcome='completed' THEN
 SELECT count(*)=4 AND coalesce(bool_and(t.state='succeeded' AND stage.graph_sealed),false) INTO terminal
 FROM (VALUES('face.detect'),('face.landmarks'),('face.parse'),('mask.face_skin')) operations(operation)
 CROSS JOIN LATERAL photo.semantic_native_input_saved_stage(p_tenant,(r.config_snapshot->>'project_id')::uuid,r.created_by,
  (r.config_snapshot->'v27_detector_prepare'->>'auth_session_id')::uuid,parent,
  photo.semantic_uuid_v5(parent,'semantic.'||operations.operation||':reference'),operations.operation) saved
 JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.id=saved.task_id
 JOIN photo.runs stage ON stage.tenant_id=t.tenant_id AND stage.id=t.run_id;
 IF NOT terminal THEN RETURN false;END IF;
 ELSIF p_outcome='failed' THEN
 -- Freeze the bounded original stage inventory until the terminal update.
 -- The wrapper already owns tenant/usage/detector locks; retries/cancellation
 -- writers cannot change a selected stage between observation and retirement.
 PERFORM t.id FROM photo.runs stage JOIN photo.tasks t ON t.tenant_id=stage.tenant_id AND t.run_id=stage.id
 WHERE stage.tenant_id=p_tenant AND stage.id IN (
  photo.semantic_uuid_v5(parent,'semantic.face.detect:reference'),
  photo.semantic_uuid_v5(parent,'semantic.mask.person:reference'),
  photo.semantic_uuid_v5(parent,'semantic.face.landmarks:reference'),
  photo.semantic_uuid_v5(parent,'semantic.face.parse:reference'),
  photo.semantic_uuid_v5(parent,'semantic.mask.face_skin:reference'),
  photo.semantic_uuid_v5(parent,'semantic.defect.prepare:reference'))
 ORDER BY stage.id,t.id FOR SHARE OF stage,t;
 SELECT EXISTS(SELECT 1 FROM photo.v27_native_initial_terminal_state(p_tenant,(r.config_snapshot->>'project_id')::uuid,r.created_by,
  (r.config_snapshot->'v27_detector_prepare'->>'auth_session_id')::uuid,parent) observed WHERE observed.operation IS NOT NULL) INTO terminal;
 IF NOT terminal THEN RETURN false;END IF;
 END IF;
 UPDATE photo.runs SET v27_initial_recovery_token=NULL,v27_initial_recovery_until=NULL,v27_initial_recovery_done=terminal,
 v27_initial_recovery_failures=CASE WHEN p_outcome='retry' THEN least(r.v27_initial_recovery_failures::bigint+1,2147483647)::integer ELSE 0 END,
 v27_initial_recovery_after=CASE WHEN terminal THEN NULL WHEN p_outcome='retry' THEN clock_timestamp()+make_interval(secs=>least(300,5*power(2,least(r.v27_initial_recovery_failures,6)))::integer) ELSE clock_timestamp()+interval '5 seconds' END
 WHERE tenant_id=p_tenant AND id=p_detector;
 RETURN true;
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
ALTER FUNCTION photo.record_v27_native_initial_pipeline(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v27_native_initial_pipeline(uuid,uuid,uuid,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION photo.record_v27_native_initial_pipeline(uuid,uuid,uuid,text) FROM photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
