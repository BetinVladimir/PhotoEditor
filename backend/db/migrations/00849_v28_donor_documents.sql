-- +goose Up
ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check
 CHECK (purpose IN ('task_params','review_selection','v2_input_manifest',
  'v2_session_plan_params','v2_analysis_input_snapshot','v2_analysis_context',
  'v2_analysis_consents','v23_training_manifest','v23_training_snapshot',
  'v23_fit_params','v23_evaluate_params','v23_frozen_base',
  'v23_frozen_approved','v23_zero_profile','v24_capture_prepare_params','v24_capture_shots','v24_capture_consents','v24_capture_context','v24_capture_assess_params','v25_lens_params','v25_lens_original_binding','v28_donor_confirmation') OR
  CASE WHEN purpose ~ '^v2_input_chunk_([0-9]|[1-9][0-9]{1,3})$'
   THEN substring(purpose from 16)::integer BETWEEN 0 AND 1953 ELSE false END OR
  CASE WHEN purpose ~ '^v2_analysis_describe_params_([0-9]|[1-3][0-9])$'
   THEN true ELSE false END);

ALTER TABLE photo.backend_documents ADD CONSTRAINT v28_donor_context_required CHECK (
 purpose<>'v28_donor_confirmation' OR v28_donor_issuer_context IS NOT NULL);
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v28_donor_document(p_tenant uuid,p_run uuid,p_owner uuid,p_context jsonb,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;capacity numeric;used numeric;aid uuid:=gen_random_uuid();fence uuid:=gen_random_uuid();
BEGIN
 IF p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RAISE EXCEPTION 'INVALID_DONOR_BUCKET' USING ERRCODE='22023';END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF photo.v28_donor_document_scope(p_tenant,p_run,p_owner,p_context,p_sha,p_bytes) IS DISTINCT FROM true THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run
  AND b.owner_id=p_owner AND b.purpose='v28_donor_confirmation' FOR UPDATE;
 IF FOUND THEN
  IF d.sha256 IS DISTINCT FROM p_sha OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket
   OR d.v28_donor_issuer_context IS DISTINCT FROM p_context THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
    AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket
    AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
    AND a.media_type='application/json' AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id
    AND a.output_role='v28_donor_confirmation'
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',d.artifact_id,fence,false;RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity OR used+p_bytes>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,v28_donor_issuer_context)
 VALUES(p_tenant,p_run,p_owner,'v28_donor_confirmation',aid,fence,p_sha,p_bytes,p_bucket,p_context);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
ALTER FUNCTION photo.reserve_v28_donor_document(uuid,uuid,uuid,jsonb,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v28_donor_document(uuid,uuid,uuid,jsonb,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v28_donor_document(uuid,uuid,uuid,jsonb,text,bigint,text) TO photo_scheduler;

CREATE FUNCTION photo.complete_v28_donor_document(p_tenant uuid,p_run uuid,p_owner uuid,p_context jsonb,
 p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;target text;
BEGIN
 IF p_token IS NULL OR p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF photo.v28_donor_document_scope(p_tenant,p_run,p_owner,p_context,p_sha,p_bytes) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run
  AND b.owner_id=p_owner AND b.purpose='v28_donor_confirmation' FOR UPDATE;
 IF NOT FOUND OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token
  OR d.sha256 IS DISTINCT FROM p_sha OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket
  OR d.v28_donor_issuer_context IS DISTINCT FROM p_context THEN RETURN false;END IF;
 target:='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a
  WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_key=target
   AND a.object_version=p_version AND a.media_type='application/json'
   AND a.output_role='v28_donor_confirmation' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,p_tenant,p_bucket,target,p_version,p_sha,p_bytes,'application/json','ready',clock_timestamp(),'v28_donor_confirmation');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,storage_bytes=storage_bytes+d.bytes
  WHERE tenant_id=p_tenant AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DONOR_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v28_donor_document(uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v28_donor_document(uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v28_donor_document(uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text,text) TO photo_scheduler;

-- Prevent the legacy generic completion route from bypassing current donor gates.
ALTER FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) RENAME TO complete_backend_document_pre_v28_donor;
REVOKE ALL ON FUNCTION photo.complete_backend_document_pre_v28_donor(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) FROM photo_scheduler;
CREATE FUNCTION photo.complete_backend_document(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_purpose='v28_donor_confirmation' THEN RETURN false;END IF;
 RETURN photo.complete_backend_document_pre_v28_donor(p_tenant,p_run,p_owner,p_purpose,p_artifact,p_token,p_sha,p_bytes,p_bucket,p_version);
END $$;
ALTER FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
