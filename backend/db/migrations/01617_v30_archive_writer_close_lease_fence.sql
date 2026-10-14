-- +goose Up
-- Retire the unfenced close entrypoint. Existing closed receipts remain intact.
REVOKE ALL ON FUNCTION photo.finish_v30_archive_complete_writer(uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementBegin
CREATE FUNCTION photo.finish_v30_archive_complete_writer(p_tenant uuid,p_upload uuid,p_writer uuid,p_completion uuid,p_receipt text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;
BEGIN
 IF p_writer IS NULL OR p_completion IS NULL OR p_receipt IS NULL OR p_receipt IN('','null') OR length(p_receipt)>1024 OR p_receipt ~ '[[:cntrl:]]' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.completion_token IS DISTINCT FROM p_completion THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_writer AND kind='complete' AND state='closed' AND receipt=p_receipt) THEN RETURN true;END IF;
 IF d.state<>'completing' OR d.completion_lease_until IS NULL OR d.completion_lease_until<=clock_timestamp() OR d.expires_at<=clock_timestamp() THEN RETURN false;END IF;
 UPDATE photo.v30_archive_writers SET state='closed',receipt=p_receipt,closed_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_writer AND kind='complete' AND state='open';
 RETURN FOUND;
END $$;
ALTER FUNCTION photo.finish_v30_archive_complete_writer(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_v30_archive_complete_writer(uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.finish_v30_archive_complete_writer(uuid,uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
