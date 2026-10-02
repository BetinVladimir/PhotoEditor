-- +goose Up
-- Function-only guest projection and cancellation for session-owned downloads.
-- +goose StatementBegin
CREATE FUNCTION photo.get_portal_download(p_session_hash bytea,p_job uuid)
RETURNS TABLE(id uuid,status text,files_done bigint,files_total bigint,bytes_written bigint,volumes jsonb,expires_at timestamptz,error jsonb)
LANGUAGE plpgsql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
DECLARE ident record;j photo.download_jobs%ROWTYPE;
BEGIN
 IF octet_length(p_session_hash)<>32 OR p_job IS NULL THEN RETURN;END IF;
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,NULL,'download');
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO j FROM photo.download_jobs x WHERE x.tenant_id=ident.tenant_id AND x.id=p_job
  AND x.share_id=ident.share_id AND x.principal_type='guest' AND x.principal_id=ident.session_id
  AND x.policy_version=ident.link_version;
 IF NOT FOUND THEN RETURN;END IF;
 RETURN QUERY SELECT j.id,j.status,
  (SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id AND i.artifact_id IS NOT NULL),
  (SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id),
  coalesce((SELECT sum(v.bytes_written) FROM photo.download_volumes v WHERE v.tenant_id=j.tenant_id AND v.download_job_id=j.id),0),
  coalesce((SELECT jsonb_agg(jsonb_build_object('number',v.volume_no,'status',v.status,'bytes',v.bytes_written,
    'delivery_kind',coalesce((SELECT CASE WHEN bool_and(i.delivery_kind='single') THEN 'single' ELSE 'zip' END
      FROM photo.download_job_items i WHERE i.tenant_id=v.tenant_id AND i.download_job_id=v.download_job_id AND i.volume_no=v.volume_no),'zip'))
    ORDER BY v.volume_no) FROM photo.download_volumes v WHERE v.tenant_id=j.tenant_id AND v.download_job_id=j.id),'[]'::jsonb),
  j.expires_at,NULL::jsonb;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.get_portal_download(bytea,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_portal_download(bytea,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.get_portal_download(bytea,uuid) TO photo_portal;

-- +goose StatementBegin
CREATE FUNCTION photo.cancel_portal_download(p_session_hash bytea,p_csrf_hash bytea,p_job uuid)
RETURNS TABLE(outcome text,id uuid,status text,files_done bigint,files_total bigint,bytes_written bigint,volumes jsonb,expires_at timestamptz,error jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ident record;j photo.download_jobs%ROWTYPE;r photo.runs%ROWTYPE;
BEGIN
 IF octet_length(p_session_hash)<>32 OR octet_length(p_csrf_hash)<>32 OR p_job IS NULL THEN
  RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::text,NULL::bigint,NULL::bigint,NULL::bigint,NULL::jsonb,NULL::timestamptz,NULL::jsonb;RETURN;
 END IF;
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,p_csrf_hash,'download');
 IF NOT FOUND THEN RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::text,NULL::bigint,NULL::bigint,NULL::bigint,NULL::jsonb,NULL::timestamptz,NULL::jsonb;RETURN;END IF;
 SELECT * INTO j FROM photo.download_jobs x WHERE x.tenant_id=ident.tenant_id AND x.id=p_job
  AND x.share_id=ident.share_id AND x.principal_type='guest' AND x.principal_id=ident.session_id
  AND x.policy_version=ident.link_version FOR UPDATE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'not_found',NULL::uuid,NULL::text,NULL::bigint,NULL::bigint,NULL::bigint,NULL::jsonb,NULL::timestamptz,NULL::jsonb;RETURN;END IF;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=j.tenant_id AND x.id=j.run_id FOR UPDATE;
 IF NOT FOUND OR r.kind<>'download' OR r.input_snapshot_id<>j.snapshot_id THEN
  RAISE EXCEPTION 'DOWNLOAD_RUN_INVARIANT' USING ERRCODE='23514';
 END IF;
 IF j.status IN ('queued','preparing','building','uploading') THEN
  UPDATE photo.download_jobs SET cancel_requested=true,status='cancelling' WHERE tenant_id=j.tenant_id AND id=j.id;
  UPDATE photo.runs SET cancel_requested=true,status=CASE WHEN status IN ('draft','queued','running','waiting_review') THEN 'cancelling' ELSE status END,
   revision=CASE WHEN status IN ('draft','queued','running','waiting_review') THEN revision+1 ELSE revision END
   WHERE tenant_id=r.tenant_id AND id=r.id;
  j.status:='cancelling';
 END IF;
 RETURN QUERY SELECT 'ok',j.id,j.status,
  (SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id AND i.artifact_id IS NOT NULL),
  (SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id),
  coalesce((SELECT sum(v.bytes_written) FROM photo.download_volumes v WHERE v.tenant_id=j.tenant_id AND v.download_job_id=j.id),0),
  coalesce((SELECT jsonb_agg(jsonb_build_object('number',v.volume_no,'status',v.status,'bytes',v.bytes_written,
    'delivery_kind',coalesce((SELECT CASE WHEN bool_and(i.delivery_kind='single') THEN 'single' ELSE 'zip' END
      FROM photo.download_job_items i WHERE i.tenant_id=v.tenant_id AND i.download_job_id=v.download_job_id AND i.volume_no=v.volume_no),'zip'))
    ORDER BY v.volume_no) FROM photo.download_volumes v WHERE v.tenant_id=j.tenant_id AND v.download_job_id=j.id),'[]'::jsonb),
  j.expires_at,NULL::jsonb;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.cancel_portal_download(bytea,bytea,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.cancel_portal_download(bytea,bytea,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.cancel_portal_download(bytea,bytea,uuid) TO photo_portal;

-- The shared task cancellation finalizer owns run termination. Keep its public
-- download projection synchronized without teaching the generic finalizer about
-- guest policy or delivery tables.
-- +goose StatementBegin
CREATE FUNCTION photo.sync_download_job_terminal() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.kind='download' AND NEW.status IN ('cancelled','failed') AND NEW.status IS DISTINCT FROM OLD.status THEN
  UPDATE photo.download_jobs SET status=NEW.status,cancel_requested=CASE WHEN NEW.status='cancelled' THEN true ELSE cancel_requested END
  WHERE tenant_id=NEW.tenant_id AND run_id=NEW.id AND status NOT IN ('ready','expired','cancelled','failed');
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.sync_download_job_terminal() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.sync_download_job_terminal() FROM PUBLIC;
CREATE TRIGGER sync_download_job_terminal AFTER UPDATE OF status ON photo.runs
FOR EACH ROW EXECUTE FUNCTION photo.sync_download_job_terminal();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
