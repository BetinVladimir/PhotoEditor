-- +goose Up
-- A cleanup receipt may return an allocation only after physical purge proof.
ALTER TABLE photo.output_uploads ADD COLUMN returned_to_run_reservation boolean NOT NULL DEFAULT false;
ALTER TABLE photo.output_uploads ADD CONSTRAINT output_refund_requires_cleaned_run
 CHECK(NOT returned_to_run_reservation OR (state='cleaned' AND run_reservation_id IS NOT NULL));
-- +goose StatementBegin
DO $$
DECLARE target record;source text;definition text;count_rows integer:=0;
 needle text:='u.run_reservation_id=q.id AND allocated.run_id=r.id';
BEGIN
 FOR target IN SELECT p.oid FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='photo' AND strpos(p.prosrc,needle)>0 LOOP
  SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc WHERE oid=target.oid
  AND prosecdef AND pg_get_userbyid(proowner)='photo_fn_owner';
  IF source IS NULL OR (length(source)-length(replace(source,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'OUTPUT_REFUND_ACCOUNTING_SOURCE_SHAPE';END IF;
  EXECUTE replace(definition,source,replace(source,needle,
   'u.run_reservation_id=q.id AND NOT u.returned_to_run_reservation AND allocated.run_id=r.id'));
  count_rows:=count_rows+1;
 END LOOP;
 IF count_rows<>8 THEN RAISE EXCEPTION 'OUTPUT_REFUND_ACCOUNTING_SOURCE_COUNT';END IF;
END $$;
DO $$
DECLARE source text;definition text;needle text:='WHERE x.state IN (''pending'',''cleaning'',''cleaned'')';
BEGIN
 SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc
 WHERE oid='photo.claim_output_cleanup()'::regprocedure AND prosecdef AND pg_get_userbyid(proowner)='photo_fn_owner';
 IF source IS NULL OR strpos(source,needle)=0 THEN RAISE EXCEPTION 'OUTPUT_CLEANUP_DISCOVERY_SOURCE_SHAPE';END IF;
 EXECUTE replace(definition,source,replace(source,needle,
  'WHERE NOT EXISTS(SELECT 1 FROM photo.artifacts published WHERE published.id=x.artifact_id AND published.tenant_id=x.tenant_id) AND x.state IN (''pending'',''cleaning'',''cleaned'')'));
END $$;
CREATE OR REPLACE FUNCTION photo.finish_output_cleanup(p_upload uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.output_uploads%ROWTYPE;t photo.tasks%ROWTYPE;u photo.output_uploads%ROWTYPE;
 r photo.runs%ROWTYPE;q photo.quota_reservations%ROWTYPE;refund boolean:=false;
BEGIN
 SELECT * INTO hint FROM photo.output_uploads WHERE id=p_upload;
 IF NOT FOUND THEN RETURN false;END IF;
 -- Canonical node/slot/tenant usage/run/task locks precede output/quota locks.
 t:=photo.lock_task(hint.task_id);
 SELECT * INTO u FROM photo.output_uploads WHERE id=p_upload AND tenant_id=t.tenant_id AND task_id=t.id FOR UPDATE;
 IF u.id IS NULL OR p_token IS NULL OR u.cleanup_lease_until IS NULL
 OR u.state NOT IN('cleaning','cleaned') OR u.cleanup_token IS DISTINCT FROM p_token
 OR u.cleanup_lease_until<=clock_timestamp()
 OR EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.id=u.artifact_id AND a.tenant_id=u.tenant_id)
 OR (t.state='running' AND t.attempt=u.attempt AND t.fencing_token=u.fence
  AND t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp()) THEN RETURN false;END IF;
 IF u.state='cleaning' OR (u.state='cleaned' AND NOT u.returned_to_run_reservation AND u.run_reservation_id IS NOT NULL) THEN
  SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
  IF u.run_reservation_id IS NOT NULL THEN
   SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=u.tenant_id AND id=u.run_reservation_id FOR UPDATE;
   refund:=q.id IS NOT NULL AND q.holder_type='run' AND q.holder_id=t.run_id
    AND q.state='reserved' AND q.expires_at>clock_timestamp()
    AND q.principal_type='user' AND q.principal_id=r.created_by AND q.share_id IS NULL AND q.files=0
    AND r.status IN('queued','running','waiting_review') AND NOT r.cancel_requested AND NOT t.cancel_requested
    AND EXISTS(SELECT 1 FROM photo.tenants WHERE id=u.tenant_id AND status='active')
    AND q.bytes::numeric+u.bytes<=9007199254740991;
  END IF;
  -- Recheck the cleanup lease after any quota lock wait. Unknown cleanup cannot settle.
  IF u.cleanup_lease_until<=clock_timestamp() THEN RETURN false;END IF;
  IF refund AND u.state='cleaned' THEN
   -- Historical cleanup already released reserved bytes. Reacquire only available capacity.
   UPDATE photo.tenant_usage usage SET reserved_bytes=reserved_bytes+u.bytes
    FROM photo.tenants tenant WHERE usage.tenant_id=u.tenant_id AND tenant.id=usage.tenant_id
    AND usage.storage_bytes::numeric+usage.reserved_bytes+u.bytes<=(tenant.quotas->>'storage_bytes')::numeric
    AND usage.storage_bytes::numeric+usage.reserved_bytes+u.bytes<=9007199254740991;
   IF NOT FOUND THEN RETURN false;END IF;
  END IF;
  IF refund THEN
   UPDATE photo.quota_reservations SET bytes=bytes+u.bytes WHERE tenant_id=q.tenant_id AND id=q.id;
   -- Allocation becomes remaining run quota; tenant reserved bytes stay unchanged.
  ELSIF u.state='cleaning' THEN
   UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-u.bytes WHERE tenant_id=u.tenant_id AND reserved_bytes>=u.bytes;
   IF NOT FOUND THEN RAISE EXCEPTION 'UPLOAD_RESERVATION_INCONSISTENT' USING ERRCODE='23514';END IF;
  END IF;
 END IF;
 UPDATE photo.output_uploads SET state='cleaned',returned_to_run_reservation=returned_to_run_reservation OR refund,
  cleanup_token=NULL,cleanup_lease_until=NULL,cleanup_after=clock_timestamp()+interval '24 hours' WHERE id=u.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.finish_output_cleanup(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_output_cleanup(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_portal,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.finish_output_cleanup(uuid,uuid) TO photo_maintenance;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
