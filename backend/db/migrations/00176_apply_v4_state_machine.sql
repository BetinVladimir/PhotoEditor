-- Generic sealed epochs for immutable Apply v4 plans. Legacy graph v3 remains
-- handled by claim_apply_build/advance_apply_build without changed semantics.
-- +goose Up
GRANT SELECT ON photo.apply_nodes,photo.tasks TO photo_fn_owner;
GRANT UPDATE ON photo.tasks,photo.runs,photo.apply_builds TO photo_fn_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_apply_build_versioned()
RETURNS TABLE(tenant_id uuid,run_id uuid,source_collection_id uuid,style_version_id uuid,phase text,item_count int,lease_token uuid,lease_until timestamptz,graph_version int,stage_index int)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE legacy record;
BEGIN
 SELECT * INTO legacy FROM photo.claim_apply_build();
 IF FOUND THEN
  RETURN QUERY SELECT legacy.tenant_id,legacy.run_id,legacy.source_collection_id,legacy.style_version_id,legacy.phase,legacy.item_count,legacy.lease_token,legacy.lease_until,b.graph_version,b.stage_index
   FROM photo.apply_builds b WHERE b.tenant_id=legacy.tenant_id AND b.run_id=legacy.run_id AND b.lease_token=legacy.lease_token;
  RETURN;
 END IF;
 RETURN QUERY WITH candidate AS (
  SELECT b.tenant_id,b.run_id FROM photo.apply_builds b
  JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
  LEFT JOIN photo.apply_v4_stages current_stage ON current_stage.tenant_id=b.tenant_id AND current_stage.run_id=b.run_id AND current_stage.stage_index=b.stage_index
  LEFT JOIN photo.apply_v4_stages previous_stage ON previous_stage.tenant_id=b.tenant_id AND previous_stage.run_id=b.run_id AND previous_stage.stage_index=b.stage_index-1
  WHERE b.graph_version=4 AND b.phase NOT IN('complete','failed') AND (b.lease_until IS NULL OR b.lease_until<clock_timestamp())
   AND r.status IN('queued','running') AND NOT r.cancel_requested
   AND ((b.phase='adopt' AND r.graph_sealed AND EXISTS(SELECT FROM photo.apply_v4_stages q WHERE q.tenant_id=b.tenant_id AND q.run_id=b.run_id AND q.phase='quality')
        AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase='quality')=b.item_count)
    OR (b.stage_index>0 AND current_stage.cardinality<>'review' AND r.graph_sealed
        AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase)=CASE previous_stage.cardinality WHEN 'item' THEN b.item_count WHEN 'aggregate' THEN 1 ELSE -1 END))
  ORDER BY b.updated_at,b.run_id FOR UPDATE OF b SKIP LOCKED LIMIT 1
 ), opened AS (
  UPDATE photo.runs r SET graph_sealed=false,revision=revision+1 FROM candidate c JOIN photo.apply_builds b ON b.tenant_id=c.tenant_id AND b.run_id=c.run_id
  WHERE r.tenant_id=c.tenant_id AND r.id=c.run_id AND r.graph_sealed AND b.phase<>'adopt' RETURNING r.id
 ), claimed AS (
  UPDATE photo.apply_builds b SET lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp()
  FROM candidate c WHERE b.tenant_id=c.tenant_id AND b.run_id=c.run_id
   AND (b.phase='adopt' OR EXISTS(SELECT FROM opened o WHERE o.id=b.run_id)) RETURNING b.*
 ) SELECT c.tenant_id,c.run_id,c.source_collection_id,c.style_version_id,c.phase,c.item_count,c.lease_token,c.lease_until,c.graph_version,c.stage_index FROM claimed c;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_apply_build_versioned() OWNER TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.advance_apply_v4_build(p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;s photo.apply_v4_stages%ROWTYPE;next_stage photo.apply_v4_stages%ROWTYPE;expected int;n int;
BEGIN
 SELECT * INTO b FROM photo.apply_builds WHERE run_id=p_run AND graph_version=4 AND lease_token=p_token AND lease_until>=clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR b.phase IN('adopt','complete','failed') OR b.stage_index IS NULL THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.apply_v4_stages WHERE tenant_id=b.tenant_id AND run_id=b.run_id AND stage_index=b.stage_index;
 IF NOT FOUND OR s.phase<>b.phase OR s.cardinality='review' THEN RETURN false;END IF;
 expected:=CASE s.cardinality WHEN 'item' THEN b.item_count WHEN 'aggregate' THEN 1 ELSE -1 END;
 IF (SELECT count(*) FROM photo.apply_nodes WHERE tenant_id=b.tenant_id AND run_id=b.run_id AND phase=s.phase)<>expected
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=b.tenant_id AND run_id=b.run_id AND operation=s.operation AND state='blocked')<>expected THEN RETURN false;END IF;
 UPDATE photo.tasks SET state='ready',next_attempt_at=clock_timestamp() WHERE tenant_id=b.tenant_id AND run_id=b.run_id AND operation=s.operation AND state='blocked';
 SELECT * INTO next_stage FROM photo.apply_v4_stages WHERE tenant_id=b.tenant_id AND run_id=b.run_id AND stage_index=b.stage_index+1;
 UPDATE photo.apply_builds SET stage_index=CASE WHEN next_stage.phase IS NULL THEN stage_index ELSE stage_index+1 END,
  phase=coalesce(next_stage.phase,'adopt'),lease_token=NULL,lease_until=NULL,last_error=NULL,updated_at=clock_timestamp()
 WHERE tenant_id=b.tenant_id AND run_id=b.run_id AND lease_token=p_token;GET DIAGNOSTICS n=ROW_COUNT;
 IF n=1 THEN UPDATE photo.runs SET graph_sealed=true,status='running',revision=revision+1 WHERE tenant_id=b.tenant_id AND id=b.run_id AND NOT graph_sealed;END IF;
 RETURN n=1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.advance_apply_v4_build(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.advance_apply_v4_build(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.advance_apply_v4_build(uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
