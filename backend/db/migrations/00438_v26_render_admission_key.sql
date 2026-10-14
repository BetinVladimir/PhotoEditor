-- +goose Up
-- Reuse the existing V2.Admit ledger; no parallel idempotency table.
-- +goose StatementBegin
CREATE FUNCTION photo.claim_v26_render_submission(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_batch uuid,p_key text,p_sha text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.idempotency_records%ROWTYPE; h bytea; expected uuid;
BEGIN
 IF p_key IS NULL OR p_key COLLATE "C" !~ '^[!-~]{1,128}$' OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
  OR p_tenant IS NULL OR p_actor IS NULL OR p_batch IS NULL THEN RETURN 'expired'; END IF;
 h:=public.digest(uuid_send(p_tenant)||convert_to('V2.Admit:'||p_actor::text||':'||p_key,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_batch<>expected THEN RETURN 'expired'; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND OR p_tenant IS NULL OR p_actor IS NULL OR p_batch IS NULL OR p_project IS NULL OR p_session IS NULL
  OR NOT coalesce(photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session),false) THEN RETURN 'expired'; END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN 'expired'; END IF;
 SELECT * INTO idem FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
  AND principal_id=p_actor AND route='V2.Admit' AND key=p_key FOR UPDATE;
 IF idem.id IS NOT NULL THEN
  IF idem.request_hash<>p_sha OR (idem.resource_id IS NOT NULL AND idem.resource_id<>p_batch) THEN RETURN 'conflict'; END IF;
  IF idem.expires_at<=clock_timestamp() AND idem.response_status IS NULL THEN RETURN 'expired'; END IF;
  IF idem.resource_id IS NULL AND idem.response_status IS NOT NULL THEN RETURN 'expired'; END IF;
  IF idem.resource_id IS NOT NULL THEN
   IF NOT EXISTS(SELECT 1 FROM photo.v26_render_submission_state(p_tenant,p_batch,p_actor,p_session) scoped WHERE scoped.request_sha=p_sha) THEN RETURN 'conflict'; END IF;
   RETURN CASE WHEN idem.response_status=202 THEN 'replayed' ELSE 'pending' END;
  END IF;
 ELSE
  -- GC of an old key never makes its deterministic existing run reusable.
  IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_batch) THEN RETURN 'expired'; END IF;
  INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at)
   VALUES(p_tenant,'user',p_actor,'V2.Admit',p_key,p_sha,clock_timestamp()+interval '24 hours')
   ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
  SELECT * INTO idem FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
   AND principal_id=p_actor AND route='V2.Admit' AND key=p_key FOR UPDATE;
  IF idem.id IS NULL THEN RETURN 'expired'; END IF;
  IF idem.request_hash<>p_sha OR idem.resource_id IS NOT NULL THEN RETURN 'conflict'; END IF;
 END IF;
 UPDATE photo.idempotency_records SET resource_id=p_batch WHERE tenant_id=p_tenant AND id=idem.id;
 RETURN 'created';
END $$;
ALTER FUNCTION photo.claim_v26_render_submission(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v26_render_submission(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_v26_render_submission(uuid,uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;

CREATE FUNCTION photo.probe_v26_render_submission(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_batch uuid,p_key text,p_sha text)
RETURNS TABLE(outcome text,run_status text,cancel_requested boolean)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.idempotency_records%ROWTYPE; scoped record; h bytea; expected uuid;
BEGIN
 IF p_key IS NULL OR p_key COLLATE "C" !~ '^[!-~]{1,128}$' OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
  OR p_tenant IS NULL OR p_actor IS NULL OR p_batch IS NULL OR p_project IS NULL OR p_session IS NULL
  OR NOT coalesce(photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session),false) THEN
  RETURN QUERY SELECT 'expired'::text,NULL::text,false; RETURN;
 END IF;
 h:=public.digest(uuid_send(p_tenant)||convert_to('V2.Admit:'||p_actor::text||':'||p_key,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_batch<>expected THEN RETURN QUERY SELECT 'expired'::text,NULL::text,false; RETURN; END IF;
 SELECT * INTO idem FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
  AND principal_id=p_actor AND route='V2.Admit' AND key=p_key;
 IF idem.id IS NULL THEN
  RETURN QUERY SELECT CASE WHEN EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_batch) THEN 'expired' ELSE 'missing' END,NULL::text,false; RETURN;
 END IF;
 IF idem.request_hash<>p_sha OR (idem.resource_id IS NOT NULL AND idem.resource_id<>p_batch) THEN
  RETURN QUERY SELECT 'conflict'::text,NULL::text,false; RETURN;
 END IF;
 IF idem.resource_id IS NULL OR (idem.expires_at<=clock_timestamp() AND idem.response_status IS NULL) THEN
  RETURN QUERY SELECT 'expired'::text,NULL::text,false; RETURN;
 END IF;
 SELECT * INTO scoped FROM photo.v26_render_submission_state(p_tenant,p_batch,p_actor,p_session);
 IF scoped.request_sha IS NULL OR scoped.request_sha<>p_sha THEN
  RETURN QUERY SELECT 'expired'::text,NULL::text,false; RETURN;
 END IF;
 RETURN QUERY SELECT CASE WHEN idem.response_status=202 THEN 'replayed' ELSE 'pending' END,scoped.run_status,scoped.cancel_requested;
END $$;
ALTER FUNCTION photo.probe_v26_render_submission(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.probe_v26_render_submission(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.probe_v26_render_submission(uuid,uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
