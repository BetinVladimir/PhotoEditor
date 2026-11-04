-- +goose Up
-- FN-D-18 (role photo_admin, owner decision) and the monthly partitions of video_egress_events (FN-D-21).

DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='photo_admin') THEN CREATE ROLE photo_admin NOLOGIN;END IF;
END $$;

-- +goose StatementBegin
CREATE FUNCTION photo.set_video_tenant_limits(p_tenant uuid,p_expected_revision bigint,p_ingest integer,p_heavy integer,p_light integer,
  p_preview integer,p_daily_cost bigint,p_daily_unlimited boolean,p_uploads integer,p_actor uuid) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE cur photo.video_tenant_limits%ROWTYPE;rev bigint;
BEGIN
 IF p_expected_revision IS NULL OR p_expected_revision<0 OR p_daily_unlimited IS NULL THEN RAISE EXCEPTION 'LIMIT_INVALID' USING ERRCODE='22023';END IF;
 IF p_daily_unlimited AND p_daily_cost IS NOT NULL THEN RAISE EXCEPTION 'LIMIT_INVALID' USING ERRCODE='22023';END IF;
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant;
 IF NOT FOUND THEN RAISE EXCEPTION 'TENANT_NOT_FOUND' USING ERRCODE='P0002';END IF;
 -- expected_revision 0 means "no override row yet"; anything else is compare-and-set
 SELECT * INTO cur FROM photo.video_tenant_limits WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN
  IF p_expected_revision<>0 THEN RAISE EXCEPTION 'REVISION_CONFLICT' USING ERRCODE='40001';END IF;
  INSERT INTO photo.video_tenant_limits(tenant_id,max_outstanding_ingest,max_outstanding_heavy,max_outstanding_light,max_outstanding_preview,
   daily_cost_seconds,daily_cost_unlimited,max_active_uploads) VALUES(p_tenant,p_ingest,p_heavy,p_light,p_preview,p_daily_cost,p_daily_unlimited,p_uploads);
  rev:=1;
 ELSE
  IF cur.revision<>p_expected_revision THEN RAISE EXCEPTION 'REVISION_CONFLICT' USING ERRCODE='40001';END IF;
  UPDATE photo.video_tenant_limits SET max_outstanding_ingest=p_ingest,max_outstanding_heavy=p_heavy,max_outstanding_light=p_light,
   max_outstanding_preview=p_preview,daily_cost_seconds=p_daily_cost,daily_cost_unlimited=p_daily_unlimited,max_active_uploads=p_uploads,
   revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=p_tenant RETURNING revision INTO rev;
 END IF;
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
 VALUES(p_tenant,'admin',p_actor,'video.tenant_limits.set','video_tenant_limits',p_tenant,gen_random_uuid(),
  jsonb_build_object('revision',rev,'max_outstanding_ingest',p_ingest,'max_outstanding_heavy',p_heavy,'max_outstanding_light',p_light,
   'max_outstanding_preview',p_preview,'daily_cost_seconds',p_daily_cost,'daily_cost_unlimited',p_daily_unlimited,'max_active_uploads',p_uploads));
 RETURN rev;
END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.set_video_tenant_limits(uuid,bigint,integer,integer,integer,integer,bigint,boolean,integer,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.set_video_tenant_limits(uuid,bigint,integer,integer,integer,integer,bigint,boolean,integer,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.set_video_tenant_limits(uuid,bigint,integer,integer,integer,integer,bigint,boolean,integer,uuid) TO photo_admin;

-- The partitioned table must belong to the function owner for partitions to be created from a SECURITY DEFINER function.
ALTER TABLE photo.video_egress_events OWNER TO photo_fn_owner;
ALTER TABLE photo.video_egress_events_default OWNER TO photo_fn_owner;

-- Partitions live in their own schema owned by the function owner, so photo_fn_owner needs no CREATE on schema photo.
CREATE SCHEMA IF NOT EXISTS photo_parts AUTHORIZATION photo_fn_owner;
REVOKE ALL ON SCHEMA photo_parts FROM PUBLIC;

-- +goose StatementBegin
-- Creates the partitions of the current month and p_ahead following months. Rows already sitting in the DEFAULT
-- partition for a month move into its new partition. Returns the number of partitions created.
CREATE FUNCTION photo.ensure_video_egress_partitions(p_ahead integer) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE m date;i integer;n integer:=0;part text;lo timestamptz;hi timestamptz;
BEGIN
 IF p_ahead IS NULL OR p_ahead NOT BETWEEN 0 AND 12 THEN RAISE EXCEPTION 'LIMIT_INVALID' USING ERRCODE='22023';END IF;
 FOR i IN 0..p_ahead LOOP
  m:=(date_trunc('month',now() AT TIME ZONE 'UTC')+make_interval(months=>i))::date;
  part:='video_egress_events_'||to_char(m,'YYYYMM');
  IF to_regclass('photo_parts.'||part) IS NOT NULL THEN CONTINUE;END IF;
  lo:=(m::timestamp AT TIME ZONE 'UTC');hi:=((m+interval '1 month')::timestamp AT TIME ZONE 'UTC');
  LOCK TABLE photo.video_egress_events_default IN ACCESS EXCLUSIVE MODE;
  EXECUTE format('CREATE TABLE photo_parts.%I (LIKE photo.video_egress_events INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)',part);
  EXECUTE format('INSERT INTO photo_parts.%I SELECT * FROM photo.video_egress_events_default WHERE at>=%L AND at<%L',part,lo,hi);
  DELETE FROM photo.video_egress_events_default WHERE at>=lo AND at<hi;
  EXECUTE format('ALTER TABLE photo_parts.%I ENABLE ROW LEVEL SECURITY',part);
  EXECUTE format('ALTER TABLE photo_parts.%I FORCE ROW LEVEL SECURITY',part);
  EXECUTE format('CREATE POLICY trusted_functions ON photo_parts.%I TO photo_fn_owner USING (true) WITH CHECK (true)',part);
  EXECUTE format('ALTER TABLE photo.video_egress_events ATTACH PARTITION photo_parts.%I FOR VALUES FROM (%L) TO (%L)',part,lo,hi);
  n:=n+1;
 END LOOP;
 RETURN n;
END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.ensure_video_egress_partitions(integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.ensure_video_egress_partitions(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.ensure_video_egress_partitions(integer) TO photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
