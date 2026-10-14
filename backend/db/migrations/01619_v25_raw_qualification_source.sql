-- +goose Up
-- Read-only physical report authority; caller holds its API transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_raw_qualification_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_report uuid,p_sha text,p_pin jsonb)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.model_qualifications q JOIN photo.artifacts a
 ON a.tenant_id=q.tenant_id AND a.id=q.report_artifact_id AND a.sha256=q.report_sha
 JOIN photo.v2_qualification_policies p ON p.operation=q.operation AND p.operation_version=q.operation_version
 AND p.backend=q.backend AND p.model_lock_sha=q.model_lock_sha AND p.runtime_lock_sha=q.runtime_lock_sha
 AND p.device_profile_sha=q.device_profile_sha AND p.dataset_sha=q.dataset_sha
 WHERE q.tenant_id=p_tenant AND q.project_id=p_project AND q.report_artifact_id=p_report AND q.report_sha=p_sha
 AND q.operation='asset.decode' AND q.operation_version='1' AND q.backend='cpu'
 AND q.trusted_import AND q.status='passed' AND p.enabled AND p.license_cleared
 AND p.model_lock_sha=p_pin->>'model_lock_sha' AND p.runtime_lock_sha=p_pin->>'runtime_lock_sha'
 AND p.device_profile_sha=p_pin->>'device_profile_sha' AND p.dataset_sha=p_pin->>'dataset_sha'
 AND p.issuer_id::text=p_pin->>'issuer_id'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.bytes BETWEEN 2 AND 8388608 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 FOR SHARE OF q,a,p;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_raw_qualification_object(uuid,uuid,uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_raw_qualification_object(uuid,uuid,uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_raw_qualification_object(uuid,uuid,uuid,uuid,uuid,text,jsonb) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
