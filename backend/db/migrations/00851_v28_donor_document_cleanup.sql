-- +goose Up
-- Fence incomplete donor publications after their current issuer scope expires.
-- Ready artifacts are exclusively handled by artifact GC / tenant deletion.
-- +goose StatementBegin
CREATE FUNCTION photo.claim_v28_donor_document_cleanup()
RETURNS TABLE(document_id uuid,cleanup_token uuid,tenant_id uuid,run_id uuid,artifact_id uuid,bucket text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.backend_documents%ROWTYPE;d photo.backend_documents%ROWTYPE;
 fence uuid:=gen_random_uuid();prior_tenant text;active_scope boolean;
BEGIN
 SELECT b.* INTO hint FROM photo.backend_documents b WHERE b.purpose='v28_donor_confirmation'
  AND b.state IN('pending','cleaning','cleaned') AND b.cleanup_after<=clock_timestamp()
  AND (b.cleanup_lease_until IS NULL OR b.cleanup_lease_until<=clock_timestamp())
  AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=b.tenant_id AND a.id=b.artifact_id)
 ORDER BY b.cleanup_after,b.id LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 -- Use the same tenant/quota/run/document lock order as reservation and completion.
 PERFORM id FROM photo.tenants WHERE id=hint.tenant_id FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=hint.tenant_id FOR UPDATE;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=hint.tenant_id AND id=hint.run_id FOR UPDATE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.id=hint.id FOR UPDATE;
 IF NOT FOUND OR d.state NOT IN('pending','cleaning','cleaned') OR d.cleanup_after>clock_timestamp()
  OR (d.cleanup_lease_until IS NOT NULL AND d.cleanup_lease_until>clock_timestamp())
  OR EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=d.tenant_id AND a.id=d.artifact_id) THEN RETURN;END IF;
 IF d.state='pending' THEN
  prior_tenant:=current_setting('app.tenant_id',true);
  PERFORM set_config('app.tenant_id',d.tenant_id::text,true);
  active_scope:=photo.v28_donor_document_scope(d.tenant_id,d.run_id,d.owner_id,d.v28_donor_issuer_context,d.sha256,d.bytes);
  PERFORM set_config('app.tenant_id',coalesce(prior_tenant,''),true);
  IF active_scope IS TRUE THEN
   -- Avoid starving later invalid candidates behind a valid pending upload.
   UPDATE photo.backend_documents SET cleanup_after=clock_timestamp()+interval '5 minutes' WHERE id=d.id;
   RETURN;
  END IF;
 END IF;
 UPDATE photo.backend_documents SET state=CASE WHEN state='cleaned' THEN 'cleaned' ELSE 'cleaning' END,
  cleanup_token=fence,cleanup_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=d.id;
 RETURN QUERY SELECT d.id,fence,d.tenant_id,d.run_id,d.artifact_id,d.bucket;
END $$;
ALTER FUNCTION photo.claim_v28_donor_document_cleanup() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v28_donor_document_cleanup() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_v28_donor_document_cleanup() TO photo_maintenance;
ALTER FUNCTION photo.claim_document_cleanup() RENAME TO claim_document_cleanup_pre_v28_donor;
REVOKE ALL ON FUNCTION photo.claim_document_cleanup_pre_v28_donor() FROM photo_maintenance;
CREATE FUNCTION photo.claim_document_cleanup()
RETURNS TABLE(document_id uuid,cleanup_token uuid,tenant_id uuid,run_id uuid,artifact_id uuid,bucket text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 RETURN QUERY SELECT * FROM photo.claim_document_cleanup_pre_v28_donor();
 IF FOUND THEN RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.claim_v28_donor_document_cleanup();
END $$;
ALTER FUNCTION photo.claim_document_cleanup() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_document_cleanup() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_document_cleanup() TO photo_maintenance;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
