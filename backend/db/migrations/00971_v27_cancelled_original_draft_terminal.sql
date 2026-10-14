-- +goose Up
-- Terminal state only. No output, registration or admission authority.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v27_native_initial_terminal_state(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid)
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
 AND t.operation_version='1'
 -- A legitimate user cancellation can precede transport registration/sealing.
 -- Only an unattempted original blocked/cancelled node may use this branch;
 -- it confers no successful producer or admission authority.
 AND (stage.graph_sealed OR (
  NOT stage.graph_sealed AND stage.cancel_requested AND stage.status IN('cancelling','cancelled')
  AND t.state IN('blocked','cancelled') AND t.attempt=0 AND t.canonical_result_id IS NULL))
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
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
