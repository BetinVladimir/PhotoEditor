-- +goose Up
-- Read-only recovery locators for original native stages. This does not grant
-- input registration, execution, completion or qualification authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_input_saved_stage(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_run uuid,p_operation text)
RETURNS TABLE(config_snapshot jsonb,config_hash text,task_id uuid,params_artifact_id uuid,params_sha text,input_digest text,execution_profile jsonb)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH selected AS (
 SELECT r.*,r.config_snapshot->CASE p_operation
  WHEN 'face.detect' THEN 'v27_detector_prepare'
  WHEN 'face.landmarks' THEN 'v27_face_prepare'
  WHEN 'face.parse' THEN 'v27_face_prepare'
  WHEN 'mask.face_skin' THEN 'v27_skin_prepare'
  WHEN 'defects.propose' THEN 'v27_defect_native_prepare'
 END AS native
 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_run
 )
 SELECT r.config_snapshot,r.config_hash,t.id,t.params_artifact_id,a.sha256,t.input_digest,
  t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha'
 FROM selected r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.id::text=r.native->>'task_id'
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND p_operation IN('face.detect','face.landmarks','face.parse','mask.face_skin','defects.propose')
 AND r.kind='v2' AND r.created_by=p_actor AND NOT r.cancel_requested AND NOT t.cancel_requested
 AND r.config_snapshot->>'project_id'=p_project::text
 AND jsonb_typeof(r.native)='object' AND r.native->'schema_version'='1'::jsonb
 AND r.native->>'auth_session_id'=p_session::text AND r.native->>'parent_run_id'=p_parent::text
 AND (p_operation='face.detect' OR r.native->>'operation'=p_operation)
 AND jsonb_typeof(r.native->'recovery')='object'
 AND t.operation=p_operation AND t.operation_version='1'
 AND t.params_artifact_id::text=r.native->'params'->>'artifact_id' AND a.sha256=r.native->'params'->>'sha256'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND t.input_digest=r.native->>'input_digest'
 AND (SELECT count(*) FROM photo.tasks other WHERE other.tenant_id=p_tenant AND other.run_id=p_run)=1
 AND octet_length(r.config_snapshot::text) BETWEEN 2 AND 4194304
 AND octet_length(t.execution_profile::text) BETWEEN 2 AND 1048576;
$$;
ALTER FUNCTION photo.semantic_native_input_saved_stage(uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_input_saved_stage(uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_input_saved_stage(uuid,uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
