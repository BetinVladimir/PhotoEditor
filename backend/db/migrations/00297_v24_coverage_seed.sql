-- +goose Up
ALTER TABLE photo.capture_assessment_jobs ADD COLUMN coverage_seed jsonb
 CHECK(coverage_seed IS NULL OR (jsonb_typeof(coverage_seed)='array' AND octet_length(coverage_seed::text)<=8388608));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_capture_coverage_seed() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.coverage_seed IS NOT NULL AND NEW.coverage_seed IS DISTINCT FROM OLD.coverage_seed THEN
  RAISE EXCEPTION 'immutable capture coverage seed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_capture_coverage_seed() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_capture_coverage_seed() FROM PUBLIC;
CREATE TRIGGER capture_coverage_immutable BEFORE UPDATE ON photo.capture_assessment_jobs FOR EACH ROW EXECUTE FUNCTION photo.guard_capture_coverage_seed();
-- +goose StatementBegin
CREATE FUNCTION photo.freeze_capture_coverage(p_tenant uuid,p_job uuid,p_token uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE job photo.capture_assessment_jobs%ROWTYPE; result jsonb; frame jsonb; identity_row jsonb;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT * INTO job FROM photo.capture_assessment_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND OR job.state<>'leased' OR job.lease_token IS DISTINCT FROM p_token OR job.lease_until<=clock_timestamp() THEN RETURN NULL; END IF;
 IF job.history_seed IS NULL THEN RAISE EXCEPTION 'V24_CAPTURE_HISTORY_UNAVAILABLE' USING ERRCODE='23514'; END IF;
 -- Explicit confirmation serializes on the same session lock.
 PERFORM id FROM photo.shoot_sessions WHERE tenant_id=p_tenant AND project_id=job.project_id AND id=job.session_id FOR SHARE;
 IF NOT FOUND THEN RETURN NULL; END IF;
 IF job.coverage_seed IS NOT NULL THEN result:=job.coverage_seed;
 ELSE
  SELECT coalesce(jsonb_agg(jsonb_build_object(
   'confirmation_id',c.id,'receipt_id',c.receipt_id,'asset_version_id',c.asset_version_id,
   'quality',a.technical_quality,'report',jsonb_build_object('artifact_id',a.report_artifact_id,'sha256',a.quality_report_sha),
   'identity_evidence',c.identity_evidence) ORDER BY c.created_at,c.id),'[]'::jsonb) INTO result
  FROM jsonb_array_elements(job.history_seed) history
  JOIN photo.capture_frame_confirmations c ON c.tenant_id=p_tenant AND c.project_id=job.project_id
   AND c.session_id=job.session_id AND c.receipt_id=(history->>'receipt_id')::uuid
   AND c.asset_version_id=(history->>'asset_version_id')::uuid AND c.asset_version_id<>job.asset_version_id
  JOIN photo.capture_alerts a ON a.tenant_id=c.tenant_id AND a.id=c.alert_id AND a.asset_version_id=c.asset_version_id
   AND a.session_id=c.session_id AND a.quality_metric='capture.technical_quality/1'
  JOIN photo.artifacts report ON report.tenant_id=a.tenant_id AND report.id=a.report_artifact_id
   AND report.sha256=a.quality_report_sha AND report.state='ready' AND report.verified_at IS NOT NULL
   AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null')
   AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp());
  IF octet_length(result::text)>8388608 OR jsonb_array_length(result)>5000 THEN
   RAISE EXCEPTION 'capture coverage evidence too large' USING ERRCODE='22023';
  END IF;
 END IF;
 -- Do not reuse cached person coverage after an identity-consent revocation.
 FOR frame IN SELECT value FROM jsonb_array_elements(result) LOOP
  FOR identity_row IN SELECT value FROM jsonb_array_elements(frame->'identity_evidence') ORDER BY value->>'person_id' LOOP
   PERFORM id FROM photo.project_persons WHERE tenant_id=p_tenant AND project_id=job.project_id
    AND id=(identity_row->>'person_id')::uuid AND state='active' FOR SHARE;
   IF NOT FOUND THEN RAISE EXCEPTION 'V24_IDENTITY_CONSENT_REVOKED' USING ERRCODE='23514'; END IF;
   PERFORM id FROM photo.consent_grants WHERE tenant_id=p_tenant AND project_id=job.project_id
    AND id=(identity_row->>'consent_id')::uuid AND person_id=(identity_row->>'person_id')::uuid
    AND purpose='identity_matching' AND status='granted' AND epoch=(identity_row->>'epoch')::bigint FOR SHARE;
   IF NOT FOUND THEN RAISE EXCEPTION 'V24_IDENTITY_CONSENT_REVOKED' USING ERRCODE='23514'; END IF;
  END LOOP;
 END LOOP;
 IF job.coverage_seed IS NULL THEN
  UPDATE photo.capture_assessment_jobs SET coverage_seed=result WHERE tenant_id=p_tenant AND id=p_job;
 END IF;
 RETURN result;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.freeze_capture_coverage(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.freeze_capture_coverage(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.freeze_capture_coverage(uuid,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
