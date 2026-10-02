-- +goose Up
ALTER TABLE photo.capture_assessment_jobs ADD COLUMN history_seed jsonb
 CHECK(history_seed IS NULL OR (jsonb_typeof(history_seed)='array' AND octet_length(history_seed::text)<=2097152));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_capture_history_seed() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.history_seed IS NOT NULL AND NEW.history_seed IS DISTINCT FROM OLD.history_seed THEN
  RAISE EXCEPTION 'immutable capture history' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_capture_history_seed() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_capture_history_seed() FROM PUBLIC;
CREATE TRIGGER capture_history_immutable BEFORE UPDATE ON photo.capture_assessment_jobs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_capture_history_seed();
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.freeze_capture_selection(p_tenant uuid,p_job uuid,p_token uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE job photo.capture_assessment_jobs%ROWTYPE; session_row photo.shoot_sessions%ROWTYPE;
 receipt photo.capture_receipts%ROWTYPE; shot jsonb; recent jsonb; history_rows jsonb; result jsonb;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT * INTO job FROM photo.capture_assessment_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND OR job.state<>'leased' OR job.lease_token IS DISTINCT FROM p_token
 OR job.lease_until<=clock_timestamp() THEN RETURN NULL; END IF;
 IF job.selection_seed IS NOT NULL THEN RETURN job.selection_seed; END IF;
 SELECT * INTO session_row FROM photo.shoot_sessions WHERE tenant_id=p_tenant AND project_id=job.project_id AND id=job.session_id FOR SHARE;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT * INTO receipt FROM photo.capture_receipts WHERE tenant_id=p_tenant AND project_id=job.project_id AND id=job.receipt_id;
 IF NOT FOUND THEN RETURN NULL; END IF;
 -- Locking the session pairs its revision with the same shot-list pointer.
 SELECT jsonb_build_object('document_id',d.id,'artifact_id',d.artifact_id,'sha256',d.sha256)
 INTO shot FROM photo.shot_lists l JOIN photo.v2_documents d
 ON d.tenant_id=l.tenant_id AND d.project_id=l.project_id AND d.id=l.document_id
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256
 WHERE l.tenant_id=p_tenant AND l.project_id=job.project_id AND l.session_id=job.session_id
 AND d.document_type='ShotList' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF shot IS NULL AND EXISTS(SELECT FROM photo.shot_lists WHERE tenant_id=p_tenant AND session_id=job.session_id) THEN
  RAISE EXCEPTION 'capture shot-list artifact unavailable' USING ERRCODE='23514';
 END IF;
 SELECT coalesce(jsonb_agg(history.content_sha ORDER BY history.created_at,history.id),'[]'::jsonb),
 coalesce(jsonb_agg(jsonb_build_object('receipt_id',history.id,'asset_version_id',history.asset_version_id,'content_sha',history.content_sha)
 ORDER BY history.created_at,history.id),'[]'::jsonb) INTO recent,history_rows
 FROM (SELECT prior.id,prior.created_at,prior.content_sha,binding.asset_version_id FROM photo.capture_receipts prior
 JOIN photo.capture_receipt_assets binding ON binding.tenant_id=prior.tenant_id AND binding.receipt_id=prior.id
 WHERE prior.tenant_id=p_tenant AND prior.project_id=job.project_id AND prior.session_id=job.session_id
 AND (prior.created_at,prior.id)<(receipt.created_at,receipt.id)
 ORDER BY prior.created_at DESC,prior.id DESC LIMIT 5000) history;
 result:=jsonb_build_object('schema_version',1,'session_id',job.session_id,
 'session_revision',session_row.revision::text,'consent_epoch',session_row.consent_epoch::text,
 'shot_document',shot,'recent_content_shas',recent);
 UPDATE photo.capture_assessment_jobs SET selection_seed=result,history_seed=history_rows WHERE tenant_id=p_tenant AND id=p_job;
 RETURN result;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.freeze_capture_selection(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.freeze_capture_selection(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.freeze_capture_selection(uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.capture_history_source(p_tenant uuid,p_job uuid,p_token uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE job photo.capture_assessment_jobs%ROWTYPE;
BEGIN
 SELECT j.* INTO job FROM photo.capture_assessment_jobs j JOIN photo.tenants t ON t.id=j.tenant_id AND t.status='active'
 WHERE j.tenant_id=p_tenant AND j.id=p_job AND j.state='leased' AND j.lease_token=p_token AND j.lease_until>clock_timestamp();
 IF NOT FOUND THEN RETURN NULL; END IF;
 IF job.history_seed IS NULL THEN RAISE EXCEPTION 'V24_CAPTURE_HISTORY_UNAVAILABLE' USING ERRCODE='23514'; END IF;
 RETURN job.history_seed;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_history_source(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_history_source(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_history_source(uuid,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
