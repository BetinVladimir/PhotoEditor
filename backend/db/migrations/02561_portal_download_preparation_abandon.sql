-- +goose Up
-- Receipt-aware reset serializes with BOTH existing guest writers. No object,
-- snapshot, job, output or quota is deleted by abandoning an unaccepted intent.
-- +goose StatementBegin
CREATE FUNCTION photo.abandon_portal_download_preparation(p_session_hash bytea,p_csrf_hash bytea,
 p_selection_key text,p_selection_hash text,p_start_key text,p_snapshot uuid,p_preset uuid)
RETURNS TABLE(outcome text,receipt jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ident record;l photo.share_links%ROWTYPE;s photo.share_sessions%ROWTYPE;
 selected photo.idempotency_records%ROWTYPE;started photo.idempotency_records%ROWTYPE;
 snap photo.snapshots%ROWTYPE;j photo.download_jobs%ROWTYPE;start_hash text;body jsonb;
BEGIN
 IF p_session_hash IS NULL OR octet_length(p_session_hash)<>32 OR p_csrf_hash IS NULL OR octet_length(p_csrf_hash)<>32
 OR p_selection_key IS NULL OR p_selection_key!~'^[!-~]{1,128}$' OR p_start_key IS NULL OR p_start_key!~'^[!-~]{1,128}$'
 OR p_selection_hash IS NULL OR p_selection_hash!~'^[0-9a-f]{64}$' OR p_preset IS NULL OR p_preset='00000000-0000-0000-0000-000000000000'::uuid THEN
  RETURN QUERY SELECT 'invalid',NULL::jsonb;RETURN;END IF;
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,p_csrf_hash,'download');
 IF NOT FOUND THEN RETURN QUERY SELECT 'unauthorized',NULL::jsonb;RETURN;END IF;
 -- Same link -> guest session -> selection receipt -> start receipt order as the
 -- real producers. The shared session lock serializes even a late first POST.
 SELECT * INTO l FROM photo.share_links WHERE tenant_id=ident.tenant_id AND id=ident.share_id FOR UPDATE;
 SELECT * INTO s FROM photo.share_sessions WHERE tenant_id=ident.tenant_id AND id=ident.session_id FOR UPDATE;
 IF l.id IS NULL OR s.id IS NULL OR s.session_hash IS DISTINCT FROM p_session_hash OR s.csrf_hash IS DISTINCT FROM p_csrf_hash
 OR s.revoked_at IS NOT NULL OR s.expires_at<=clock_timestamp() OR l.revoked_at IS NOT NULL
 OR l.expires_at<=clock_timestamp() OR l.policy_version IS DISTINCT FROM s.link_version
 OR l.permissions->'download' IS DISTINCT FROM 'true'::jsonb THEN
  RETURN QUERY SELECT 'unauthorized',NULL::jsonb;RETURN;END IF;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at)
 VALUES(s.tenant_id,'guest',s.id,'Portal.Selection',p_selection_key,p_selection_hash,'9999-12-31T00:00:00Z')
 ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO selected FROM photo.idempotency_records WHERE tenant_id=s.tenant_id AND principal_type='guest' AND principal_id=s.id AND route='Portal.Selection' AND key=p_selection_key FOR UPDATE;
 IF selected.request_hash IS DISTINCT FROM p_selection_hash THEN RETURN QUERY SELECT 'conflict',NULL::jsonb;RETURN;END IF;
 IF selected.response_status=201 THEN
  SELECT * INTO snap FROM photo.snapshots WHERE tenant_id=s.tenant_id AND id=selected.resource_id AND principal_type='guest' AND principal_id=s.id AND collection_id=s.collection_id AND kind='selection';
  IF snap.id IS NULL OR selected.response_body->>'id' IS DISTINCT FROM snap.id::text
  OR selected.response_body->>'digest' IS DISTINCT FROM snap.digest OR selected.response_body->>'item_count' IS DISTINCT FROM snap.item_count::text
  OR p_snapshot IS NOT NULL AND p_snapshot IS DISTINCT FROM snap.id THEN RETURN QUERY SELECT 'conflict',NULL::jsonb;RETURN;END IF;
 ELSIF selected.response_status IS NOT NULL AND (selected.response_status=410 AND selected.resource_id IS NULL
   AND selected.response_body->>'status'='abandoned' AND selected.response_body->>'selection_key'=p_selection_key
   AND selected.response_body->>'start_key'=p_start_key AND selected.response_body->>'preset_id'=p_preset::text
   AND selected.response_body->>'collection_id'=s.collection_id::text) IS DISTINCT FROM TRUE THEN
  RETURN QUERY SELECT 'conflict',NULL::jsonb;RETURN;
 ELSIF p_snapshot IS NOT NULL THEN RETURN QUERY SELECT 'conflict',NULL::jsonb;RETURN;END IF;
 -- Exact canonical Go DownloadRequest encoding (lexicographically ordered keys).
 start_hash:=CASE WHEN snap.id IS NULL THEN repeat('0',64) ELSE encode(public.digest(
  '{"preset_id":"'||p_preset::text||'","selection_snapshot_id":"'||snap.id::text||'"}','sha256'),'hex') END;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at)
 VALUES(s.tenant_id,'guest',s.id,'Portal.Download.Start',p_start_key,start_hash,'9999-12-31T00:00:00Z')
 ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO started FROM photo.idempotency_records WHERE tenant_id=s.tenant_id AND principal_type='guest' AND principal_id=s.id AND route='Portal.Download.Start' AND key=p_start_key FOR UPDATE;
 IF started.request_hash IS DISTINCT FROM start_hash THEN RETURN QUERY SELECT 'conflict',NULL::jsonb;RETURN;END IF;
 IF started.response_status=202 THEN
  SELECT * INTO j FROM photo.download_jobs WHERE tenant_id=s.tenant_id AND id=started.resource_id AND principal_type='guest' AND principal_id=s.id AND collection_id=s.collection_id AND snapshot_id=snap.id AND preset->>'id'=p_preset::text;
  IF j.id IS NULL OR started.response_body IS DISTINCT FROM jsonb_build_object('download_job_id',j.id,'status','queued') THEN RETURN QUERY SELECT 'conflict',NULL::jsonb;RETURN;END IF;
 ELSIF started.response_status IS NOT NULL AND (started.response_status=410 AND started.resource_id IS NULL
  AND started.response_body->>'status'='abandoned' AND started.response_body->>'selection_key'=p_selection_key
  AND started.response_body->>'start_key'=p_start_key AND started.response_body->>'preset_id'=p_preset::text
  AND started.response_body->>'collection_id'=s.collection_id::text) IS DISTINCT FROM TRUE THEN RETURN QUERY SELECT 'conflict',NULL::jsonb;RETURN;END IF;
 -- Recheck live authority AFTER every possible receipt wait, before mutation or
 -- delivery of an accepted receipt. A renewed/different guest never shares keys.
 IF NOT EXISTS(SELECT FROM photo.authorize_portal_session(p_session_hash,p_csrf_hash,'download') x
  WHERE x.tenant_id=s.tenant_id AND x.session_id=s.id AND x.share_id=l.id)
 OR s.expires_at<=clock_timestamp() OR l.expires_at<=clock_timestamp() THEN RETURN QUERY SELECT 'unauthorized',NULL::jsonb;RETURN;END IF;
 IF j.id IS NOT NULL THEN RETURN QUERY SELECT 'accepted',started.response_body;RETURN;END IF;
 body:=jsonb_build_object('status','abandoned','selection_key',p_selection_key,'start_key',p_start_key,'preset_id',p_preset,'collection_id',s.collection_id);
 UPDATE photo.idempotency_records SET expires_at='9999-12-31T00:00:00Z',response_status=410,response_body=body,resource_id=NULL WHERE tenant_id=s.tenant_id AND id=started.id;
 IF selected.response_status IS NULL OR selected.response_status=410 THEN
  UPDATE photo.idempotency_records SET expires_at='9999-12-31T00:00:00Z',response_status=410,response_body=body,resource_id=NULL WHERE tenant_id=s.tenant_id AND id=selected.id;
 ELSE UPDATE photo.idempotency_records SET expires_at=greatest(expires_at,'9999-12-31T00:00:00Z') WHERE tenant_id=s.tenant_id AND id=selected.id;END IF;
 RETURN QUERY SELECT 'abandoned',body;
END $$;
ALTER FUNCTION photo.abandon_portal_download_preparation(bytea,bytea,text,text,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.abandon_portal_download_preparation(bytea,bytea,text,text,text,uuid,uuid) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_portal;
GRANT EXECUTE ON FUNCTION photo.abandon_portal_download_preparation(bytea,bytea,text,text,text,uuid,uuid) TO photo_portal;
-- Normal late producer calls recognize only this scoped receipt, and never
-- interpret it as an accepted snapshot/job or overwrite its immutable hash.
DO $$ DECLARE sig text;source text;anchor text;replacement text;BEGIN
 FOREACH sig IN ARRAY ARRAY['photo.create_portal_selection(bytea,bytea,text,text,uuid,bigint,text,uuid[],uuid,timestamptz)','photo.start_portal_download(bytea,bytea,text,text,uuid,uuid,uuid,uuid)'] LOOP
  source:=pg_get_functiondef(sig::regprocedure);
  anchor:=' IF idem.response_status IS NOT NULL THEN';
  IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'PORTAL_ABANDON_SOURCE_SHAPE %',sig;END IF;
  IF sig LIKE 'photo.create_portal_selection%' THEN
   replacement:=$a$ IF idem.response_status=410 THEN RETURN QUERY SELECT 'abandoned',NULL::uuid,NULL::text,NULL::bigint,NULL::timestamptz;RETURN;END IF;
 IF idem.response_status IS NOT NULL THEN$a$;
  ELSE
   replacement:=$a$ IF idem.response_status=410 THEN RETURN QUERY SELECT 'abandoned',NULL::uuid,NULL::text;RETURN;END IF;
 IF idem.response_status IS NOT NULL THEN$a$;
  END IF;
  EXECUTE replace(source,anchor,replacement);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '02561 is forward-only: abandoned intent identities must remain fenced';END $$;
-- +goose StatementEnd
