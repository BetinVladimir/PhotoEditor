-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.create_portal_download_intent(p_session_hash bytea,p_csrf_hash bytea,p_key text,p_request_hash text,p_job uuid,p_volume integer,p_intent uuid,p_token_hash bytea,p_expires timestamptz)
RETURNS TABLE(outcome text,intent_id uuid,expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ident record;j photo.download_jobs%ROWTYPE;v photo.download_volumes%ROWTYPE;idem photo.idempotency_records%ROWTYPE;existing photo.download_intents%ROWTYPE;
BEGIN
 IF octet_length(p_session_hash)<>32 OR octet_length(p_csrf_hash)<>32 OR p_key !~ '^[!-~]{1,128}$' OR p_request_hash !~ '^[0-9a-f]{64}$' OR p_job IS NULL OR p_volume NOT BETWEEN 1 AND 5000 OR p_intent IS NULL OR octet_length(p_token_hash)<>32
 OR p_expires IS NULL OR p_expires<=clock_timestamp() OR p_expires>clock_timestamp()+interval '60 seconds' THEN RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::timestamptz;RETURN;END IF;
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,p_csrf_hash,'download');IF NOT FOUND THEN RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::timestamptz;RETURN;END IF;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at)
 VALUES(ident.tenant_id,'guest',ident.session_id,'Portal.Download.Intent',p_key,p_request_hash,clock_timestamp()+interval '30 days') ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO idem FROM photo.idempotency_records x WHERE x.tenant_id=ident.tenant_id AND x.principal_type='guest' AND x.principal_id=ident.session_id AND x.route='Portal.Download.Intent' AND x.key=p_key FOR UPDATE;
 IF idem.request_hash<>p_request_hash THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::timestamptz;RETURN;END IF;
 IF idem.response_status IS NOT NULL THEN SELECT * INTO existing FROM photo.download_intents x WHERE x.tenant_id=ident.tenant_id AND x.id=idem.resource_id AND x.principal_id=ident.session_id;
  IF NOT FOUND THEN RAISE EXCEPTION 'DOWNLOAD_INTENT_IDEMPOTENCY' USING ERRCODE='23514';END IF;RETURN QUERY SELECT 'replayed',existing.id,existing.expires_at;RETURN;END IF;
 SELECT * INTO j FROM photo.download_jobs x WHERE x.tenant_id=ident.tenant_id AND x.id=p_job AND x.share_id=ident.share_id AND x.principal_type='guest' AND x.principal_id=ident.session_id
 AND x.policy_version=ident.link_version AND x.status='ready' AND NOT x.cancel_requested AND x.expires_at>clock_timestamp() FOR SHARE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'not_ready',NULL::uuid,NULL::timestamptz;RETURN;END IF;
 SELECT * INTO v FROM photo.download_volumes x WHERE x.tenant_id=j.tenant_id AND x.download_job_id=j.id AND x.volume_no=p_volume AND x.status='ready' AND x.artifact_id IS NOT NULL FOR SHARE;
 IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=v.tenant_id AND a.id=v.artifact_id AND a.state='ready' AND a.expires_at>clock_timestamp() AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')) THEN
  RETURN QUERY SELECT 'not_ready',NULL::uuid,NULL::timestamptz;RETURN;END IF;
 INSERT INTO photo.download_intents(id,tenant_id,principal_type,principal_id,share_id,artifact_id,policy_version,token_hash,expires_at)
 VALUES(p_intent,j.tenant_id,'guest',ident.session_id,ident.share_id,v.artifact_id,ident.link_version,p_token_hash,least(p_expires,j.expires_at));
 UPDATE photo.idempotency_records SET response_status=201,response_body=jsonb_build_object('intent_id',p_intent,'expires_at',least(p_expires,j.expires_at)),resource_id=p_intent WHERE tenant_id=ident.tenant_id AND id=idem.id;
 RETURN QUERY SELECT 'created',p_intent,least(p_expires,j.expires_at);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_portal_download_intent(bytea,bytea,text,text,uuid,integer,uuid,bytea,timestamptz) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_portal_download_intent(bytea,bytea,text,text,uuid,integer,uuid,bytea,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_portal_download_intent(bytea,bytea,text,text,uuid,integer,uuid,bytea,timestamptz) TO photo_portal;

-- Consumption precedes URL signing. A transient signer failure requires a new
-- intent; the same bearer can never be replayed.
-- +goose StatementBegin
CREATE FUNCTION photo.redeem_portal_download_intent(p_token_hash bytea,p_bucket text)
RETURNS TABLE(outcome text,object_key text,object_version text,sha256 text,bytes bigint,media_type text,filename text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE i photo.download_intents%ROWTYPE;s photo.share_sessions%ROWTYPE;l photo.share_links%ROWTYPE;a photo.artifacts%ROWTYPE;v photo.download_volumes%ROWTYPE;j photo.download_jobs%ROWTYPE;
BEGIN
 IF octet_length(p_token_hash)<>32 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN QUERY SELECT 'invalid',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
 SELECT * INTO i FROM photo.download_intents x WHERE x.token_hash=p_token_hash FOR UPDATE;
 IF NOT FOUND OR i.consumed_at IS NOT NULL OR i.expires_at<=clock_timestamp() OR i.principal_type<>'guest' THEN RETURN QUERY SELECT 'invalid',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
 SELECT * INTO s FROM photo.share_sessions x WHERE x.tenant_id=i.tenant_id AND x.id=i.principal_id FOR SHARE;
 SELECT * INTO l FROM photo.share_links x WHERE x.tenant_id=i.tenant_id AND x.id=i.share_id FOR SHARE;
 IF s.id IS NULL OR l.id IS NULL OR s.revoked_at IS NOT NULL OR s.expires_at<=clock_timestamp() OR l.revoked_at IS NOT NULL OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp())
 OR s.link_version<>i.policy_version OR l.policy_version<>i.policy_version OR l.permissions->'download'<>'true'::jsonb THEN RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
 SELECT volume.* INTO v FROM photo.download_volumes volume WHERE volume.tenant_id=i.tenant_id AND volume.artifact_id=i.artifact_id AND volume.status='ready';
 SELECT job.* INTO j FROM photo.download_jobs job WHERE job.tenant_id=i.tenant_id AND job.id=v.download_job_id AND job.principal_type='guest' AND job.principal_id=i.principal_id AND job.share_id=i.share_id
 AND job.policy_version=i.policy_version AND job.status='ready' AND job.expires_at>clock_timestamp();
 SELECT * INTO a FROM photo.artifacts x WHERE x.tenant_id=i.tenant_id AND x.id=i.artifact_id AND x.bucket=p_bucket AND x.state='ready' AND x.expires_at>clock_timestamp() AND x.object_version IS NOT NULL AND x.object_version NOT IN ('','null');
 IF v.id IS NULL OR j.id IS NULL OR a.id IS NULL THEN RETURN QUERY SELECT 'revoked',NULL::text,NULL::text,NULL::text,NULL::bigint,NULL::text,NULL::text;RETURN;END IF;
 UPDATE photo.download_intents SET consumed_at=clock_timestamp() WHERE tenant_id=i.tenant_id AND id=i.id;
 RETURN QUERY SELECT 'ok',a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,
  CASE WHEN a.media_type='application/zip' THEN 'photos-'||lpad(v.volume_no::text,3,'0')||'.zip'
   ELSE (SELECT entry_name FROM photo.download_job_items x WHERE x.tenant_id=v.tenant_id AND x.download_job_id=v.download_job_id AND x.volume_no=v.volume_no LIMIT 1) END;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.redeem_portal_download_intent(bytea,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.redeem_portal_download_intent(bytea,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.redeem_portal_download_intent(bytea,text) TO photo_portal;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
