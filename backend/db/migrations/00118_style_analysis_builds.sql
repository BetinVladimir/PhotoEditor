-- Durable, fenced coordinator for the three-stage style-analysis DAG.
-- Registration, color fitting and aggregation are published independently
-- because each stage needs exact immutable outputs from the prior stage.

-- +goose Up

CREATE TABLE photo.style_analysis_builds (
  tenant_id uuid NOT NULL,
  run_id uuid NOT NULL,
  style_id uuid NOT NULL,
  phase text NOT NULL,
  pair_count int NOT NULL,
  lease_token uuid,
  lease_until timestamptz,
  last_error text,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id,run_id),
  FOREIGN KEY (tenant_id,run_id) REFERENCES photo.runs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,style_id) REFERENCES photo.styles(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK (phase IN ('registration','color_fit','aggregate','complete','failed')),
  CHECK (pair_count BETWEEN 1 AND 32),
  CHECK ((lease_token IS NULL)=(lease_until IS NULL))
);

ALTER TABLE photo.style_analysis_builds ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.style_analysis_builds FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.style_analysis_builds TO photo_fn_owner USING (true) WITH CHECK (true);

-- +goose StatementBegin
CREATE FUNCTION photo.claim_style_analysis_build()
RETURNS TABLE(tenant_id uuid,run_id uuid,style_id uuid,phase text,pair_count int,lease_token uuid,lease_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 RETURN QUERY
 WITH candidate AS (
  SELECT b.tenant_id,b.run_id FROM photo.style_analysis_builds b
  JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
  WHERE b.phase NOT IN ('complete','failed') AND (b.lease_until IS NULL OR b.lease_until<clock_timestamp())
   AND r.status IN ('queued','running') AND NOT r.cancel_requested
   AND ((b.phase='registration' AND NOT r.graph_sealed)
    OR (b.phase='color_fit' AND (NOT r.graph_sealed OR
      (SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=b.tenant_id AND t.run_id=b.run_id AND t.operation='pair.register' AND t.state='succeeded')=b.pair_count))
    OR (b.phase='aggregate' AND (NOT r.graph_sealed OR
      (SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=b.tenant_id AND t.run_id=b.run_id AND t.operation='pair.fit.color' AND t.state='succeeded')=b.pair_count)))
  ORDER BY b.updated_at,b.run_id FOR UPDATE OF b SKIP LOCKED LIMIT 1
 ), opened AS (
  UPDATE photo.runs r SET graph_sealed=false,revision=revision+1 FROM candidate c
  WHERE r.tenant_id=c.tenant_id AND r.id=c.run_id AND r.graph_sealed RETURNING r.id
 ), claimed AS (
  UPDATE photo.style_analysis_builds b SET lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp()
  FROM candidate c WHERE b.tenant_id=c.tenant_id AND b.run_id=c.run_id
   AND (b.phase='registration' OR EXISTS(SELECT FROM opened o WHERE o.id=b.run_id)
    OR EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=b.tenant_id AND r.id=b.run_id AND NOT r.graph_sealed))
  RETURNING b.*
 ) SELECT c.tenant_id,c.run_id,c.style_id,c.phase,c.pair_count,c.lease_token,c.lease_until FROM claimed c;
END
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_style_analysis_build() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_style_analysis_build() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_style_analysis_build() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.renew_style_analysis_build(p_run uuid,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n int;
BEGIN
 UPDATE photo.style_analysis_builds SET lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp()
 WHERE run_id=p_run AND lease_token=p_token AND lease_until>=clock_timestamp() AND phase NOT IN ('complete','failed');
 GET DIAGNOSTICS n=ROW_COUNT;RETURN n=1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.renew_style_analysis_build(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_style_analysis_build(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.renew_style_analysis_build(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.advance_style_analysis_build(p_run uuid,p_token uuid,p_from text,p_to text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n int;
BEGIN
 IF p_to IS DISTINCT FROM (CASE p_from WHEN 'registration' THEN 'color_fit' WHEN 'color_fit' THEN 'aggregate' WHEN 'aggregate' THEN 'complete' END) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.style_analysis_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
  WHERE b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>=clock_timestamp() AND b.phase=p_from
   AND NOT r.graph_sealed AND NOT r.cancel_requested AND r.status IN ('queued','running')) THEN RETURN false;END IF;
 IF (p_from IN ('registration','color_fit') AND
    (SELECT count(*) FROM photo.tasks t JOIN photo.style_analysis_builds b ON b.tenant_id=t.tenant_id AND b.run_id=t.run_id
     WHERE t.run_id=p_run AND t.operation=(CASE p_from WHEN 'registration' THEN 'pair.register' ELSE 'pair.fit.color' END) AND t.state='blocked')
      IS DISTINCT FROM (SELECT pair_count FROM photo.style_analysis_builds WHERE run_id=p_run))
  OR (p_from='aggregate' AND (SELECT count(*) FROM photo.tasks WHERE run_id=p_run AND operation='style.aggregate' AND state='blocked')<>1) THEN RETURN false;END IF;
 UPDATE photo.tasks SET state='ready',next_attempt_at=clock_timestamp() WHERE run_id=p_run AND state='blocked'
  AND operation=(CASE p_from WHEN 'registration' THEN 'pair.register' WHEN 'color_fit' THEN 'pair.fit.color' ELSE 'style.aggregate' END);
 UPDATE photo.style_analysis_builds SET phase=p_to,lease_token=NULL,lease_until=NULL,last_error=NULL,updated_at=clock_timestamp()
 WHERE run_id=p_run AND lease_token=p_token AND lease_until>=clock_timestamp() AND phase=p_from
  AND p_to IN ('registration','color_fit','aggregate','complete');
 GET DIAGNOSTICS n=ROW_COUNT;
 IF n=1 THEN
  UPDATE photo.runs SET graph_sealed=true,status='running',revision=revision+1 WHERE id=p_run;
 END IF;
 RETURN n=1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.advance_style_analysis_build(uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.advance_style_analysis_build(uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.advance_style_analysis_build(uuid,uuid,text,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.fail_style_analysis_build(p_run uuid,p_token uuid,p_error text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n int;
BEGIN
 UPDATE photo.style_analysis_builds SET phase='failed',lease_token=NULL,lease_until=NULL,last_error=left(p_error,2000),updated_at=clock_timestamp()
 WHERE run_id=p_run AND lease_token=p_token AND lease_until>=clock_timestamp();
 GET DIAGNOSTICS n=ROW_COUNT;
 IF n=1 THEN UPDATE photo.runs SET status='failed',graph_sealed=true,revision=revision+1 WHERE id=p_run; END IF;
 RETURN n=1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.fail_style_analysis_build(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_style_analysis_build(uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.fail_style_analysis_build(uuid,uuid,text) TO photo_scheduler;

-- +goose Down
-- Forward-only production migrations. Restore a backup for destructive rollback.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
