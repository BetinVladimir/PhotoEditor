-- Open fully-bound defects reviews as a durable Apply v4 review stage. This
-- migration deliberately does not advance the stage after decisions; branch
-- continuation remains a separate fenced compiler step.
-- +goose Up
ALTER TABLE photo.apply_builds DROP CONSTRAINT apply_builds_phase_check;
ALTER TABLE photo.apply_builds ADD CONSTRAINT apply_builds_phase_check CHECK(phase ~ '^[a-z][a-z0-9_]{0,63}$');

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
   AND r.status IN('queued','running','waiting_review') AND NOT r.cancel_requested
   AND ((b.phase='adopt' AND r.status IN('queued','running') AND r.graph_sealed
        AND EXISTS(SELECT FROM photo.apply_v4_stages q WHERE q.tenant_id=b.tenant_id AND q.run_id=b.run_id AND q.phase='quality')
        AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded'
             WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase='quality')=b.item_count)
    OR (b.stage_index>0 AND current_stage.cardinality<>'review' AND r.status IN('queued','running') AND r.graph_sealed
        AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded'
             WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase)=CASE previous_stage.cardinality WHEN 'item' THEN b.item_count WHEN 'aggregate' THEN 1 ELSE -1 END)
    OR (b.stage_index>0 AND current_stage.phase='defects_review' AND current_stage.cardinality='review'
        AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded'
             WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase)=b.item_count
        AND (SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id
             WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND v.kind='defects' AND n.phase=previous_stage.phase)<b.item_count))
  ORDER BY b.updated_at,b.run_id FOR UPDATE OF b SKIP LOCKED LIMIT 1
 ), opened AS (
  UPDATE photo.runs r SET graph_sealed=false,status=CASE WHEN r.status='waiting_review' THEN 'running' ELSE r.status END,revision=revision+1
  FROM candidate c JOIN photo.apply_builds b ON b.tenant_id=c.tenant_id AND b.run_id=c.run_id
  WHERE r.tenant_id=c.tenant_id AND r.id=c.run_id AND r.graph_sealed AND b.phase<>'adopt' RETURNING r.id
 ), claimed AS (
  UPDATE photo.apply_builds b SET lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp()
  FROM candidate c WHERE b.tenant_id=c.tenant_id AND b.run_id=c.run_id
   AND (b.phase='adopt' OR EXISTS(SELECT FROM opened o WHERE o.id=b.run_id)
        OR EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=b.tenant_id AND r.id=b.run_id AND NOT r.graph_sealed)) RETURNING b.*
 ) SELECT c.tenant_id,c.run_id,c.source_collection_id,c.style_version_id,c.phase,c.item_count,c.lease_token,c.lease_until,c.graph_version,c.stage_index FROM claimed c;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_apply_build_versioned() OWNER TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.open_apply_v4_defect_reviews(p_tenant uuid,p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;s photo.apply_v4_stages%ROWTYPE;previous photo.apply_v4_stages%ROWTYPE;n photo.apply_nodes%ROWTYPE;
 resolved_id uuid;review_id uuid;opened int:=0;
BEGIN
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR b.graph_version<>4 OR b.phase<>'defects_review' OR b.lease_token IS DISTINCT FROM p_token
  OR b.lease_until<clock_timestamp() OR b.stage_index IS NULL THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index;
 SELECT * INTO previous FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index-1;
 IF s.phase IS DISTINCT FROM 'defects_review' OR s.cardinality IS DISTINCT FROM 'review'
  OR previous.operation IS DISTINCT FROM 'defects.propose'
  OR (SELECT count(*) FROM photo.apply_nodes pn JOIN photo.tasks pt ON pt.tenant_id=pn.tenant_id AND pt.id=pn.id
      WHERE pn.tenant_id=p_tenant AND pn.run_id=p_run AND pn.phase=previous.phase AND pt.state='succeeded' AND pt.canonical_result_id IS NOT NULL)<>b.item_count THEN RETURN false;END IF;
 FOR n IN SELECT * FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND phase=previous.phase ORDER BY ordinal LOOP
  SELECT a.id INTO resolved_id FROM photo.apply_nodes rn
   JOIN photo.tasks rt ON rt.tenant_id=rn.tenant_id AND rt.id=rn.id AND rt.state='succeeded' AND rt.canonical_result_id IS NOT NULL
   JOIN photo.task_results rr ON rr.tenant_id=rt.tenant_id AND rr.id=rt.canonical_result_id AND rr.task_id=rt.id
   JOIN photo.artifacts a ON a.tenant_id=rt.tenant_id AND a.created_by_task_id=rt.id AND a.produced_attempt=rr.attempt AND a.produced_fence=rr.fencing_token
    AND a.output_role='resolved_style' AND a.state='ready' AND rr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
   WHERE rn.tenant_id=p_tenant AND rn.run_id=p_run AND rn.phase='resolve' AND rn.ordinal=n.ordinal AND rn.source_item_id=n.source_item_id;
  IF resolved_id IS NULL OR n.params->'image'->>'artifact_id' IS NULL THEN RETURN false;END IF;
  review_id:=photo.create_defect_review(p_tenant,p_run,n.id,(n.params->'image'->>'artifact_id')::uuid,resolved_id);
  IF review_id IS NULL THEN RETURN false;END IF;opened:=opened+1;resolved_id:=NULL;
 END LOOP;
 IF opened<>b.item_count OR (SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes pn ON pn.tenant_id=v.tenant_id AND pn.id=v.task_id
    WHERE v.tenant_id=p_tenant AND v.run_id=p_run AND v.kind='defects' AND pn.phase=previous.phase)<>b.item_count THEN RETURN false;END IF;
 UPDATE photo.apply_builds SET lease_token=NULL,lease_until=NULL,last_error=NULL,updated_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND run_id=p_run AND lease_token=p_token;
 IF NOT FOUND THEN RETURN false;END IF;
 UPDATE photo.runs SET status='waiting_review',graph_sealed=false WHERE tenant_id=p_tenant AND id=p_run AND NOT cancel_requested;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation THEN RETURN false;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.open_apply_v4_defect_reviews(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.open_apply_v4_defect_reviews(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.open_apply_v4_defect_reviews(uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
