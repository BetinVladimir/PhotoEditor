-- +goose Up
-- Read-only authenticated locator; original report bytes are verified in Go.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_analysis_qualification_source(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_operation text,p_profile jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE profile jsonb:=p_profile; result jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_operation IS NULL OR p_operation NOT IN('depth.predict','mask.person')
 OR profile IS NULL OR jsonb_typeof(profile) IS DISTINCT FROM 'object' OR pg_column_size(profile)>16384
 OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.users WHERE id=p_actor FOR SHARE;
 PERFORM user_id FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=p_actor FOR SHARE;
 PERFORM user_id FROM photo.project_acl WHERE tenant_id=p_tenant AND project_id=p_project AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.sessions WHERE id=p_session AND user_id=p_actor FOR SHARE;
 IF NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('contract','Report','artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,'object_version',a.object_version,'bytes',a.bytes) INTO result FROM photo.v2_qualification_policies p
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=p_project AND q.operation=p.operation AND q.operation_version=p.operation_version
 JOIN photo.artifacts a ON a.tenant_id=q.tenant_id AND a.id=q.report_artifact_id AND a.sha256=q.report_sha
 WHERE p.operation=p_operation AND p.operation_version='1' AND p.enabled AND p.admission_enabled AND p.license_cleared
 AND p.backend=profile->>'backend' AND p.model_lock_sha=profile->>'model_lock_sha' AND p.runtime_lock_sha=profile->>'runtime_lock_sha' AND p.device_profile_sha=profile->>'device_profile_sha'
 AND q.backend=p.backend AND q.model_lock_sha=p.model_lock_sha AND q.runtime_lock_sha=p.runtime_lock_sha AND q.device_profile_sha=p.device_profile_sha AND q.dataset_sha=p.dataset_sha
 AND q.status='passed' AND q.trusted_import AND a.id=(profile->'qualification'->>'artifact_id')::uuid AND a.sha256=profile->'qualification'->>'sha256'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=q.tenant_id AND log.action='v2_qualification.imported' AND log.resource_id=q.id AND log.resource_type='model_qualification' AND log.details->>'policy_id'=p.id::text)
 ORDER BY p.id LIMIT 1 FOR SHARE OF p,q,a;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v28_relight_analysis_qualification_source(uuid,uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_analysis_qualification_source(uuid,uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_relight_analysis_qualification_source(uuid,uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
