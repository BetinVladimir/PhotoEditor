-- +goose Up
-- FN-D-19/20: stream traffic of a viewer is paid from leased budget. lease_video_egress takes up to `want` bytes out of the
-- tenant's month budget (EGRESS_PLAN_MULTIPLIER x storage_bytes of the plan) and out of the share's own budget when the share
-- carries limits.egress_bytes; settle_video_egress returns the unused rest and books what was used into the hourly counters.

-- +goose StatementBegin
CREATE FUNCTION photo.lease_video_egress(p_tenant uuid,p_share uuid,p_want bigint) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE l photo.share_links%ROWTYPE;per date:=date_trunc('month',clock_timestamp() AT TIME ZONE 'UTC')::date;plan bigint;
 pr photo.video_egress_period%ROWTYPE;sb photo.video_share_budget%ROWTYPE;granted bigint;lid uuid;share_limit bigint;
BEGIN
 IF p_want IS NULL OR p_want<=0 THEN RAISE EXCEPTION 'VIDEO_EGRESS_WANT' USING ERRCODE='22023';END IF;
 SELECT * INTO l FROM photo.share_links WHERE tenant_id=p_tenant AND id=p_share AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp());
 IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.collections c WHERE c.tenant_id=p_tenant AND c.id=l.collection_id AND c.content_kind='video' AND c.state='published' AND c.deleted_at IS NULL) THEN
  RAISE EXCEPTION 'VIDEO_SHARE_UNAVAILABLE' USING ERRCODE='P0002';END IF;
 SELECT 10*coalesce((quotas->>'storage_bytes')::bigint,0) INTO plan FROM photo.tenants WHERE id=p_tenant;
 INSERT INTO photo.video_egress_period(tenant_id,period_start,limit_bytes) VALUES(p_tenant,per,coalesce(plan,0)) ON CONFLICT DO NOTHING;
 SELECT * INTO pr FROM photo.video_egress_period WHERE tenant_id=p_tenant AND period_start=per FOR UPDATE;
 granted:=least(p_want,pr.limit_bytes-pr.consumed_bytes-pr.leased_bytes);
 share_limit:=CASE WHEN l.limits ? 'egress_bytes' AND (l.limits->>'egress_bytes') ~ '^[0-9]{1,15}$' THEN (l.limits->>'egress_bytes')::bigint END;
 IF share_limit IS NOT NULL THEN
  INSERT INTO photo.video_share_budget(tenant_id,share_id,limit_bytes) VALUES(p_tenant,p_share,share_limit) ON CONFLICT DO NOTHING;
  SELECT * INTO sb FROM photo.video_share_budget WHERE tenant_id=p_tenant AND share_id=p_share FOR UPDATE;
  granted:=least(granted,sb.limit_bytes-sb.consumed_bytes-sb.leased_bytes);
 END IF;
 IF granted<=0 THEN RAISE EXCEPTION 'VIDEO_EGRESS_LIMIT' USING ERRCODE='53400';END IF;
 UPDATE photo.video_egress_period SET leased_bytes=leased_bytes+granted WHERE tenant_id=p_tenant AND period_start=per;
 IF share_limit IS NOT NULL THEN UPDATE photo.video_share_budget SET leased_bytes=leased_bytes+granted WHERE tenant_id=p_tenant AND share_id=p_share;END IF;
 INSERT INTO photo.video_egress_leases(tenant_id,share_id,period_start,granted_bytes,expires_at) VALUES(p_tenant,p_share,per,granted,clock_timestamp()+interval '30 seconds') RETURNING id INTO lid;
 RETURN jsonb_build_object('lease_id',lid,'granted',granted);
END $$;
-- +goose StatementEnd

-- A repeat of the same lease is a no-op (the row is gone after the first settle). Bytes used beyond the grant are booked
-- as the grant: the budget invariant consumed+leased<=limit cannot be exceeded from the outside.
-- +goose StatementBegin
CREATE FUNCTION photo.settle_video_egress(p_tenant uuid,p_lease uuid,p_used bigint,p_render uuid,p_kind text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE le photo.video_egress_leases%ROWTYPE;used bigint;
BEGIN
 IF p_used IS NULL OR p_used<0 OR p_kind IS DISTINCT FROM 'stream' THEN RAISE EXCEPTION 'VIDEO_EGRESS_SETTLE' USING ERRCODE='22023';END IF;
 SELECT * INTO le FROM photo.video_egress_leases WHERE tenant_id=p_tenant AND id=p_lease FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 used:=least(p_used,le.granted_bytes);
 UPDATE photo.video_egress_period SET leased_bytes=leased_bytes-le.granted_bytes,consumed_bytes=consumed_bytes+used WHERE tenant_id=p_tenant AND period_start=le.period_start;
 UPDATE photo.video_share_budget SET leased_bytes=leased_bytes-le.granted_bytes,consumed_bytes=consumed_bytes+used WHERE tenant_id=p_tenant AND share_id=le.share_id;
 IF used>0 AND EXISTS(SELECT FROM photo.video_renders WHERE tenant_id=p_tenant AND id=p_render) THEN
  INSERT INTO photo.video_egress_counters(tenant_id,share_id,render_id,kind,bucket_start,bytes,requests)
   VALUES(p_tenant,le.share_id,p_render,'stream',date_trunc('hour',clock_timestamp()),used,1)
   ON CONFLICT(tenant_id,share_id,render_id,kind,bucket_start) DO UPDATE SET bytes=video_egress_counters.bytes+EXCLUDED.bytes,requests=video_egress_counters.requests+1;
 END IF;
 DELETE FROM photo.video_egress_leases WHERE tenant_id=p_tenant AND id=p_lease;
 RETURN true;
END $$;
-- +goose StatementEnd

-- Leases nobody settled (a crashed process) are booked as consumed once expired: the conservative rule of the spec.
-- +goose StatementBegin
CREATE FUNCTION photo.expire_video_egress_leases(p_limit integer) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE x record;n integer:=0;
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 1000 THEN RAISE EXCEPTION 'LIMIT_INVALID' USING ERRCODE='22023';END IF;
 FOR x IN SELECT tenant_id,id,granted_bytes FROM photo.video_egress_leases WHERE expires_at<=clock_timestamp() ORDER BY expires_at LIMIT p_limit FOR UPDATE SKIP LOCKED LOOP
  IF photo.settle_video_egress(x.tenant_id,x.id,x.granted_bytes,NULL,'stream') THEN n:=n+1;END IF;
 END LOOP;
 RETURN n;
END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.lease_video_egress(uuid,uuid,bigint) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.settle_video_egress(uuid,uuid,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lease_video_egress(uuid,uuid,bigint),photo.settle_video_egress(uuid,uuid,bigint,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lease_video_egress(uuid,uuid,bigint),photo.settle_video_egress(uuid,uuid,bigint,uuid,text) TO photo_portal;
ALTER FUNCTION photo.expire_video_egress_leases(integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.expire_video_egress_leases(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.expire_video_egress_leases(integer) TO photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
