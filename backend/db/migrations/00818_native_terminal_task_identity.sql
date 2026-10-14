-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v27_native_manual_child_terminal(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_config_sha text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.runs child JOIN photo.tasks t ON t.tenant_id=child.tenant_id AND t.run_id=child.id
 WHERE child.tenant_id=p_tenant AND child.id=photo.semantic_uuid_v5(p_parent,'semantic.defect.prepare:reference')
 AND child.kind='v2' AND child.created_by=p_actor AND child.config_snapshot->>'project_id'=p_project::text
 AND child.config_snapshot->'v27_defect_native_prepare'->>'parent_run_id'=p_parent::text
 AND child.config_snapshot->'v27_defect_native_prepare'->>'auth_session_id'=p_session::text
 AND child.config_snapshot->'v27_defect_native_prepare'->'request_sha'=original.config_snapshot->'v27_detector_prepare'->'request_sha'
 AND child.config_snapshot->'v27_defect_native_prepare'->'schema_version'='1'::jsonb
 AND child.config_snapshot->'v27_defect_native_prepare'->>'operation'='defects.propose'
 AND child.config_snapshot->'v27_defect_native_prepare'->>'task_id'=t.id::text
 AND t.operation='defects.propose' AND t.operation_version='1'
 AND (SELECT count(*) FROM photo.tasks other WHERE other.tenant_id=p_tenant AND other.run_id=child.id)=1
 AND (t.state IN('failed','cancelled','skipped') OR child.status IN('failed','cancelled','cancelling')))
 FROM photo.semantic_native_input_saved_stage(p_tenant,p_project,p_actor,p_session,p_parent,photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference'),'face.detect') original
 JOIN photo.runs detector ON detector.tenant_id=p_tenant AND detector.id=photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference')
 WHERE original.config_hash=p_config_sha AND detector.v27_initial_manual_selection IS NOT NULL;
$$;
ALTER FUNCTION photo.v27_native_manual_child_terminal(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_native_manual_child_terminal(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_native_manual_child_terminal(uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
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
 SELECT EXISTS(SELECT 1 FROM photo.runs stage JOIN photo.tasks t ON t.tenant_id=stage.tenant_id AND t.run_id=stage.id
 WHERE stage.tenant_id=p_tenant AND stage.created_by=r.created_by AND stage.kind='v2'
 AND stage.config_snapshot->>'project_id'=r.config_snapshot->>'project_id'
 AND t.operation IN('face.detect','face.landmarks','face.parse','mask.face_skin','defects.propose')
 AND (t.operation<>'defects.propose' OR r.v27_initial_manual_selection IS NOT NULL) AND t.operation_version='1'
 AND (SELECT count(*) FROM photo.tasks other WHERE other.tenant_id=stage.tenant_id AND other.run_id=stage.id)=1
 AND stage.id=photo.semantic_uuid_v5(parent,CASE WHEN t.operation='defects.propose' THEN 'semantic.defect.prepare:reference' ELSE 'semantic.'||t.operation||':reference' END)
 AND stage.config_snapshot->CASE t.operation WHEN 'face.detect' THEN 'v27_detector_prepare' WHEN 'mask.face_skin' THEN 'v27_skin_prepare' WHEN 'defects.propose' THEN 'v27_defect_native_prepare' ELSE 'v27_face_prepare' END->>'parent_run_id'=parent::text
 AND stage.config_snapshot->CASE t.operation WHEN 'face.detect' THEN 'v27_detector_prepare' WHEN 'mask.face_skin' THEN 'v27_skin_prepare' WHEN 'defects.propose' THEN 'v27_defect_native_prepare' ELSE 'v27_face_prepare' END->>'auth_session_id'=r.config_snapshot->'v27_detector_prepare'->>'auth_session_id'
 AND stage.config_snapshot->CASE t.operation WHEN 'face.detect' THEN 'v27_detector_prepare' WHEN 'mask.face_skin' THEN 'v27_skin_prepare' WHEN 'defects.propose' THEN 'v27_defect_native_prepare' ELSE 'v27_face_prepare' END->'request_sha'=r.config_snapshot->'v27_detector_prepare'->'request_sha'
 AND stage.config_snapshot->CASE t.operation WHEN 'face.detect' THEN 'v27_detector_prepare' WHEN 'mask.face_skin' THEN 'v27_skin_prepare' WHEN 'defects.propose' THEN 'v27_defect_native_prepare' ELSE 'v27_face_prepare' END->'schema_version'='1'::jsonb
 AND stage.config_snapshot->CASE t.operation WHEN 'face.detect' THEN 'v27_detector_prepare' WHEN 'mask.face_skin' THEN 'v27_skin_prepare' WHEN 'defects.propose' THEN 'v27_defect_native_prepare' ELSE 'v27_face_prepare' END->>'task_id'=t.id::text
 AND (t.operation='face.detect' OR stage.config_snapshot->CASE t.operation WHEN 'mask.face_skin' THEN 'v27_skin_prepare' WHEN 'defects.propose' THEN 'v27_defect_native_prepare' ELSE 'v27_face_prepare' END->>'operation'=t.operation)
 AND (t.state IN('failed','cancelled','skipped') OR stage.status IN('failed','cancelled','cancelling'))) INTO terminal;
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
