-- Reconcile decided defect branches into the immutable Apply v4 item stream.
-- Repair keeps its canonical task output; base and skip use the review-bound
-- base artifact. Skip is retained through quality and excluded at adoption.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_apply_build_versioned()
RETURNS TABLE(tenant_id uuid,run_id uuid,source_collection_id uuid,style_version_id uuid,phase text,item_count int,lease_token uuid,lease_until timestamptz,graph_version int,stage_index int)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE legacy record;
BEGIN
 SELECT * INTO legacy FROM photo.claim_apply_build();
 IF FOUND THEN
  RETURN QUERY SELECT legacy.tenant_id,legacy.run_id,legacy.source_collection_id,legacy.style_version_id,legacy.phase,legacy.item_count,legacy.lease_token,legacy.lease_until,b.graph_version,b.stage_index
   FROM photo.apply_builds b WHERE b.tenant_id=legacy.tenant_id AND b.run_id=legacy.run_id AND b.lease_token=legacy.lease_token;RETURN;
 END IF;
 RETURN QUERY WITH candidate AS (
  SELECT b.tenant_id,b.run_id FROM photo.apply_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
  LEFT JOIN photo.apply_v4_stages current_stage ON current_stage.tenant_id=b.tenant_id AND current_stage.run_id=b.run_id AND current_stage.stage_index=b.stage_index
  LEFT JOIN photo.apply_v4_stages previous_stage ON previous_stage.tenant_id=b.tenant_id AND previous_stage.run_id=b.run_id AND previous_stage.stage_index=b.stage_index-1
  WHERE b.graph_version=4 AND b.phase NOT IN('complete','failed') AND (b.lease_until IS NULL OR b.lease_until<clock_timestamp()) AND NOT r.cancel_requested
   AND ((b.phase='adopt' AND r.status IN('queued','running') AND r.graph_sealed
        AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase='quality')=b.item_count)
    OR (b.stage_index>0 AND current_stage.cardinality<>'review' AND r.status IN('queued','running') AND r.graph_sealed
        AND ((previous_stage.cardinality IN('item','aggregate') AND
              (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded'
               WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase)=CASE previous_stage.cardinality WHEN 'item' THEN b.item_count ELSE 1 END)
          OR (previous_stage.phase='defects_review' AND previous_stage.cardinality='review'
              AND (SELECT count(*) FROM photo.defect_branches d JOIN photo.reviews v ON v.tenant_id=d.tenant_id AND v.id=d.review_id
                   WHERE d.tenant_id=b.tenant_id AND d.run_id=b.run_id AND v.state='decided'
                    AND (v.decision_route<>'repair' OR EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=d.tenant_id AND t.id=d.task_id AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL)))=b.item_count)))
    OR (b.stage_index>0 AND current_stage.phase='defects_review' AND current_stage.cardinality='review' AND r.status IN('running','waiting_review')
        AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded'
             WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase)=b.item_count
        AND ((SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id
              WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND v.kind='defects' AND n.phase=previous_stage.phase)<b.item_count
          OR ((SELECT count(*) FROM photo.defect_branches d WHERE d.tenant_id=b.tenant_id AND d.run_id=b.run_id)=b.item_count
              AND (EXISTS(SELECT FROM photo.graph_expansions e WHERE e.tenant_id=b.tenant_id AND e.run_id=b.run_id AND e.state='materialized')
                OR NOT EXISTS(SELECT FROM photo.defect_branches d JOIN photo.reviews v ON v.tenant_id=d.tenant_id AND v.id=d.review_id
                              WHERE d.tenant_id=b.tenant_id AND d.run_id=b.run_id AND v.decision_route='repair'
                               AND EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=d.tenant_id AND t.id=d.task_id AND t.state<>'succeeded')))))))
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
CREATE FUNCTION photo.process_apply_v4_defect_reviews(p_tenant uuid,p_run uuid,p_token uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;s photo.apply_v4_stages%ROWTYPE;next_stage photo.apply_v4_stages%ROWTYPE;
 d photo.defect_branches%ROWTYPE;t photo.tasks%ROWTYPE;review_count int;branch_count int;activated boolean;
BEGIN
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO s FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index;
 IF b.run_id IS NULL OR b.graph_version<>4 OR b.phase<>'defects_review' OR s.cardinality<>'review'
  OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() THEN RETURN 'rejected';END IF;
 SELECT count(*) INTO review_count FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id
  WHERE v.tenant_id=p_tenant AND v.run_id=p_run AND v.kind='defects' AND n.phase='defects_propose';
 IF review_count<b.item_count THEN
  IF photo.open_apply_v4_defect_reviews(p_tenant,p_run,p_token) THEN RETURN 'opened';END IF;RETURN 'rejected';
 END IF;
 IF review_count<>b.item_count OR EXISTS(SELECT FROM photo.reviews WHERE tenant_id=p_tenant AND run_id=p_run AND kind='defects' AND state<>'decided') THEN RETURN 'rejected';END IF;
 SELECT count(*) INTO branch_count FROM photo.defect_branches WHERE tenant_id=p_tenant AND run_id=p_run;
 IF branch_count<>b.item_count THEN RETURN 'rejected';END IF;
 IF EXISTS(SELECT FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run AND state='materialized') THEN
  IF EXISTS(SELECT FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run AND state<>'materialized') THEN RETURN 'rejected';END IF;
  UPDATE photo.graph_expansions SET state='closed' WHERE tenant_id=p_tenant AND run_id=p_run AND state='materialized';
  UPDATE photo.runs SET graph_sealed=true,status='running',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run AND NOT cancel_requested;
  IF NOT FOUND THEN RETURN 'rejected';END IF;
  FOR d IN SELECT * FROM photo.defect_branches WHERE tenant_id=p_tenant AND run_id=p_run AND task_id IS NOT NULL ORDER BY review_id LOOP
   SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=d.task_id;
   IF t.id IS NULL OR t.state NOT IN('blocked','ready') THEN RAISE EXCEPTION 'DEFECT_ACTIVATION_REJECTED' USING ERRCODE='23514';END IF;
   activated:=photo.activate_bound_task(t.id,t.input_digest,t.params_artifact_id);
   IF NOT activated THEN RAISE EXCEPTION 'DEFECT_ACTIVATION_REJECTED' USING ERRCODE='23514';END IF;
  END LOOP;
  UPDATE photo.apply_builds SET lease_token=NULL,lease_until=NULL,last_error=NULL,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run AND lease_token=p_token;
  RETURN CASE WHEN FOUND THEN 'activated' ELSE 'rejected' END;
 END IF;
 IF EXISTS(SELECT FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run AND state<>'closed')
  OR EXISTS(SELECT FROM photo.defect_branches d JOIN photo.reviews v ON v.tenant_id=d.tenant_id AND v.id=d.review_id
            LEFT JOIN photo.tasks t ON t.tenant_id=d.tenant_id AND t.id=d.task_id
            WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND v.decision_route='repair' AND (t.id IS NULL OR t.state<>'succeeded' OR t.canonical_result_id IS NULL)) THEN RETURN 'rejected';END IF;
 SELECT * INTO next_stage FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index+1;
 IF next_stage.phase IS NULL OR next_stage.cardinality='review' THEN RETURN 'rejected';END IF;
 UPDATE photo.apply_builds SET stage_index=stage_index+1,phase=next_stage.phase,lease_token=NULL,lease_until=NULL,last_error=NULL,updated_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND run_id=p_run AND lease_token=p_token;
 RETURN CASE WHEN FOUND THEN 'advanced' ELSE 'rejected' END;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.process_apply_v4_defect_reviews(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.process_apply_v4_defect_reviews(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.process_apply_v4_defect_reviews(uuid,uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.apply_v4_defect_outputs(p_tenant uuid,p_run uuid,p_token uuid)
RETURNS TABLE(ordinal int,item_id uuid,task_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text,route text)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT n.ordinal,n.source_item_id,coalesce(d.task_id,n.id),a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type,v.decision_route
 FROM photo.apply_builds b JOIN photo.apply_v4_stages current_stage ON current_stage.tenant_id=b.tenant_id AND current_stage.run_id=b.run_id AND current_stage.stage_index=b.stage_index
 JOIN photo.apply_nodes n ON n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase='defects_propose'
 JOIN photo.reviews v ON v.tenant_id=n.tenant_id AND v.run_id=n.run_id AND v.task_id=n.id AND v.kind='defects' AND v.state='decided'
 JOIN photo.defect_branches d ON d.tenant_id=v.tenant_id AND d.run_id=v.run_id AND d.review_id=v.id
 LEFT JOIN photo.tasks rt ON rt.tenant_id=d.tenant_id AND rt.id=d.task_id AND rt.state='succeeded' AND rt.canonical_result_id IS NOT NULL
 LEFT JOIN photo.task_results rr ON rr.tenant_id=rt.tenant_id AND rr.id=rt.canonical_result_id AND rr.task_id=rt.id
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=CASE WHEN v.decision_route='repair' THEN
   (SELECT x.id FROM photo.artifacts x WHERE x.tenant_id=rt.tenant_id AND x.created_by_task_id=rt.id AND x.produced_attempt=rr.attempt AND x.produced_fence=rr.fencing_token
    AND x.output_role='working_image' AND rr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',x.id,'sha256',x.sha256)) LIMIT 1)
   ELSE v.base_artifact_id END
  AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.graph_version=4 AND b.lease_token=p_token AND b.lease_until>=clock_timestamp()
  AND current_stage.stage_index>(SELECT stage_index FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND phase='defects_review')
 ORDER BY n.ordinal
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.apply_v4_defect_outputs(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v4_defect_outputs(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.apply_v4_defect_outputs(uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
