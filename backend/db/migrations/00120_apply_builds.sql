-- Durable expansion state for apply runs. The API commits only the immutable
-- input/output reservation; scheduler task publication is a separate lease.

-- +goose Up
CREATE TABLE photo.apply_builds (
 tenant_id uuid NOT NULL,
 run_id uuid NOT NULL,
 source_collection_id uuid NOT NULL,
 style_version_id uuid NOT NULL,
 phase text NOT NULL DEFAULT 'resolve',
 item_count int NOT NULL,
 lease_token uuid,
 lease_until timestamptz,
 last_error text,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,run_id),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,source_collection_id) REFERENCES photo.collections(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,style_version_id) REFERENCES photo.style_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 CHECK(phase IN ('mask','stats','resolve','color','local','adopt','complete','failed')),
 CHECK(item_count BETWEEN 1 AND 5000),
 CHECK((lease_token IS NULL)=(lease_until IS NULL))
);
ALTER TABLE photo.apply_builds ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.apply_builds FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.apply_builds TO photo_fn_owner USING(true) WITH CHECK(true);

-- +goose StatementBegin
CREATE FUNCTION photo.claim_apply_build()
RETURNS TABLE(tenant_id uuid,run_id uuid,source_collection_id uuid,style_version_id uuid,phase text,item_count int,lease_token uuid,lease_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 RETURN QUERY WITH candidate AS (
  SELECT b.tenant_id,b.run_id FROM photo.apply_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
  WHERE b.phase NOT IN ('complete','failed') AND (b.lease_until IS NULL OR b.lease_until<clock_timestamp())
   AND r.status IN ('queued','running') AND NOT r.cancel_requested
   AND ((b.phase='mask' AND NOT r.graph_sealed)
    OR (b.phase='stats' AND (NOT r.graph_sealed OR (SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=b.tenant_id AND t.run_id=b.run_id AND t.operation='mask.person' AND t.state='succeeded')=b.item_count))
    OR (b.phase='resolve' AND (NOT r.graph_sealed OR (SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=b.tenant_id AND t.run_id=b.run_id AND t.operation='color.stats' AND t.state='succeeded')=b.item_count))
    OR (b.phase='color' AND (NOT r.graph_sealed OR (SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=b.tenant_id AND t.run_id=b.run_id AND t.operation='style.resolve' AND t.state='succeeded')=b.item_count))
    OR (b.phase='local' AND (NOT r.graph_sealed OR (SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=b.tenant_id AND t.run_id=b.run_id AND t.operation='image.color' AND t.state='succeeded')=b.item_count))
    OR (b.phase='adopt' AND r.graph_sealed AND (SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=b.tenant_id AND t.run_id=b.run_id AND t.operation='image.local' AND t.state='succeeded')=b.item_count))
  ORDER BY b.updated_at,b.run_id FOR UPDATE OF b SKIP LOCKED LIMIT 1
 ), opened AS (
  UPDATE photo.runs r SET graph_sealed=false,revision=revision+1 FROM candidate c JOIN photo.apply_builds b ON b.tenant_id=c.tenant_id AND b.run_id=c.run_id
  WHERE r.tenant_id=c.tenant_id AND r.id=c.run_id AND r.graph_sealed AND b.phase IN ('stats','resolve','color','local') RETURNING r.id
 ), claimed AS (
  UPDATE photo.apply_builds b SET lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp()
  FROM candidate c WHERE b.tenant_id=c.tenant_id AND b.run_id=c.run_id
   AND (b.phase IN ('mask','adopt') OR EXISTS(SELECT FROM opened o WHERE o.id=b.run_id)
    OR EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=b.tenant_id AND r.id=b.run_id AND NOT r.graph_sealed)) RETURNING b.*
 ) SELECT c.tenant_id,c.run_id,c.source_collection_id,c.style_version_id,c.phase,c.item_count,c.lease_token,c.lease_until FROM claimed c;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_apply_build() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_apply_build() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_apply_build() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.renew_apply_build(p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n int;BEGIN
 UPDATE photo.apply_builds SET lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp()
 WHERE run_id=p_run AND lease_token=p_token AND lease_until>=clock_timestamp() AND phase NOT IN ('complete','failed');
 GET DIAGNOSTICS n=ROW_COUNT;RETURN n=1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.renew_apply_build(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_apply_build(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.renew_apply_build(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.advance_apply_build(p_run uuid,p_token uuid,p_from text,p_to text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n int;expected int;op text;
BEGIN
 IF p_to IS DISTINCT FROM (CASE p_from WHEN 'mask' THEN 'stats' WHEN 'stats' THEN 'resolve' WHEN 'resolve' THEN 'color' WHEN 'color' THEN 'local' WHEN 'local' THEN 'adopt' WHEN 'adopt' THEN 'complete' END) THEN RETURN false;END IF;
 IF p_from='adopt' THEN RETURN false;END IF;
 op:=CASE p_from WHEN 'mask' THEN 'mask.person' WHEN 'stats' THEN 'color.stats' WHEN 'resolve' THEN 'style.resolve' WHEN 'color' THEN 'image.color' ELSE 'image.local' END;
 SELECT item_count INTO expected FROM photo.apply_builds WHERE run_id=p_run AND lease_token=p_token AND lease_until>=clock_timestamp() AND phase=p_from;
 IF expected IS NULL OR (SELECT count(*) FROM photo.tasks WHERE run_id=p_run AND operation=op AND state='blocked')<>expected THEN RETURN false;END IF;
 UPDATE photo.tasks SET state='ready',next_attempt_at=clock_timestamp() WHERE run_id=p_run AND operation=op AND state='blocked';
 UPDATE photo.apply_builds SET phase=p_to,lease_token=NULL,lease_until=NULL,last_error=NULL,updated_at=clock_timestamp()
 WHERE run_id=p_run AND lease_token=p_token AND lease_until>=clock_timestamp() AND phase=p_from;
 GET DIAGNOSTICS n=ROW_COUNT;
 IF n=1 THEN UPDATE photo.runs SET graph_sealed=true,status='running',revision=revision+1 WHERE id=p_run AND NOT graph_sealed;END IF;
 RETURN n=1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.advance_apply_build(uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.advance_apply_build(uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.advance_apply_build(uuid,uuid,text,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.fail_apply_build(p_run uuid,p_token uuid,p_error text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n int;q photo.quota_reservations%ROWTYPE;
BEGIN
 UPDATE photo.apply_builds SET phase='failed',lease_token=NULL,lease_until=NULL,last_error=left(p_error,2000),updated_at=clock_timestamp()
 WHERE run_id=p_run AND lease_token=p_token AND lease_until>=clock_timestamp();GET DIAGNOSTICS n=ROW_COUNT;
 IF n=1 THEN
  SELECT * INTO q FROM photo.quota_reservations WHERE holder_type='run' AND holder_id=p_run AND state='reserved' FOR UPDATE;
  IF FOUND THEN UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=q.tenant_id AND reserved_bytes>=q.bytes;UPDATE photo.quota_reservations SET bytes=0,state='released' WHERE id=q.id;END IF;
  UPDATE photo.runs SET status='failed',graph_sealed=true,revision=revision+1 WHERE id=p_run;
 END IF;RETURN n=1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.fail_apply_build(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_apply_build(uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.fail_apply_build(uuid,uuid,text) TO photo_scheduler;

-- +goose Down
-- Forward-only production migrations. Restore a backup for destructive rollback.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
