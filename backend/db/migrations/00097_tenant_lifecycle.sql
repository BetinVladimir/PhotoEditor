-- +goose Up
-- Tenant creation must cross tenant RLS once: the function verifies a live global
-- session, creates the aggregate, owner membership and usage atomically, and
-- stores replay state in global auth idempotency storage.
-- +goose StatementBegin
CREATE FUNCTION photo.create_tenant(p_user uuid,p_session uuid,p_tenant uuid,p_name text,p_quotas jsonb,p_retention jsonb,p_key text,p_request_hash text)
RETURNS TABLE(outcome text,tenant_id uuid,name text,revision bigint,quotas jsonb,retention jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.auth_idempotency_records%ROWTYPE;t photo.tenants%ROWTYPE;scope bytea;
BEGIN
 IF p_user IS NULL OR p_session IS NULL OR p_tenant IS NULL OR p_name IS NULL OR length(p_name) NOT BETWEEN 1 AND 120
 OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_key !~ '^[!-~]+$' OR p_request_hash !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_quotas)<>'object' OR jsonb_typeof(p_retention)<>'object' THEN
  RAISE EXCEPTION 'INVALID_TENANT_CREATE' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT FROM photo.sessions s JOIN photo.users u ON u.id=s.user_id
  WHERE s.id=p_session AND s.user_id=p_user AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND u.status='active') THEN
  RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::text,NULL::bigint,NULL::jsonb,NULL::jsonb;RETURN;END IF;
 scope:=digest('tenant-create:'||p_user::text,'sha256');
 INSERT INTO photo.auth_idempotency_records(scope_hash,key,request_hash,response,expires_at)
 VALUES(scope,p_key,p_request_hash,'{}',clock_timestamp()+interval '30 days') ON CONFLICT(scope_hash,key) DO NOTHING;
 SELECT * INTO idem FROM photo.auth_idempotency_records x WHERE x.scope_hash=scope AND x.key=p_key FOR UPDATE;
 IF idem.request_hash<>p_request_hash THEN
  RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text,NULL::bigint,NULL::jsonb,NULL::jsonb;RETURN;END IF;
 IF idem.response<>'{}'::jsonb THEN
  RETURN QUERY SELECT 'replayed',(idem.response->>'id')::uuid,idem.response->>'name',(idem.response->>'revision')::bigint,idem.response->'quotas',idem.response->'retention';RETURN;END IF;
 INSERT INTO photo.tenants(id,name,owner_user_id,quotas,retention) VALUES(p_tenant,p_name,p_user,p_quotas,p_retention) RETURNING * INTO t;
 INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES(t.id,p_user,'owner','active');
 INSERT INTO photo.tenant_usage(tenant_id) VALUES(t.id);
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
 VALUES(t.id,'user',p_user,'tenant.create','tenant',t.id,gen_random_uuid(),jsonb_build_object('name',t.name));
 UPDATE photo.auth_idempotency_records SET response=jsonb_build_object('id',t.id,'name',t.name,'revision',t.revision,'quotas',t.quotas,'retention',t.retention) WHERE id=idem.id;
 RETURN QUERY SELECT 'created',t.id,t.name,t.revision,t.quotas,t.retention;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_tenant(uuid,uuid,uuid,text,jsonb,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_tenant(uuid,uuid,uuid,text,jsonb,jsonb,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_tenant(uuid,uuid,uuid,text,jsonb,jsonb,text,text) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.request_tenant_deletion(p_user uuid,p_session uuid,p_tenant uuid,p_expected bigint,p_job uuid)
RETURNS TABLE(outcome text,job_id uuid) LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tenants%ROWTYPE;existing uuid;
BEGIN
 SELECT x.* INTO t FROM photo.tenants x JOIN photo.memberships m ON m.tenant_id=x.id AND m.user_id=p_user AND m.role='owner' AND m.status='active'
 JOIN photo.sessions s ON s.id=p_session AND s.user_id=p_user AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
 WHERE x.id=p_tenant FOR UPDATE OF x;
 IF NOT FOUND THEN RETURN QUERY SELECT 'not_found',NULL::uuid;RETURN;END IF;
 IF t.status='deleting' THEN SELECT id INTO existing FROM photo.deletion_jobs WHERE tenant_id=t.id AND resource_type='tenant' AND resource_id=t.id ORDER BY created_at LIMIT 1;RETURN QUERY SELECT 'deleting',existing;RETURN;END IF;
 IF t.status<>'active' THEN RETURN QUERY SELECT 'conflict',NULL::uuid;RETURN;END IF;
 IF t.revision<>p_expected THEN RETURN QUERY SELECT 'revision_conflict',NULL::uuid;RETURN;END IF;
 UPDATE photo.tenants SET status='deleting',revision=revision+1 WHERE id=t.id;
 UPDATE photo.share_links SET revoked_at=coalesce(revoked_at,clock_timestamp()),policy_version=policy_version+1 WHERE tenant_id=t.id AND revoked_at IS NULL;
 UPDATE photo.share_sessions SET revoked_at=coalesce(revoked_at,clock_timestamp()) WHERE tenant_id=t.id AND revoked_at IS NULL;
 UPDATE photo.runs SET cancel_requested=true,status=CASE WHEN status='queued' THEN 'cancelling' ELSE status END,revision=revision+1
  WHERE tenant_id=t.id AND status IN ('queued','running','waiting_review');
 INSERT INTO photo.deletion_jobs(id,tenant_id,resource_type,resource_id,requested_by) VALUES(p_job,t.id,'tenant',t.id,p_user);
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
 VALUES(t.id,'user',p_user,'tenant.delete_requested','tenant',t.id,gen_random_uuid(),jsonb_build_object('job_id',p_job));
 RETURN QUERY SELECT 'queued',p_job;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.request_tenant_deletion(uuid,uuid,uuid,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.request_tenant_deletion(uuid,uuid,uuid,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.request_tenant_deletion(uuid,uuid,uuid,bigint,uuid) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
