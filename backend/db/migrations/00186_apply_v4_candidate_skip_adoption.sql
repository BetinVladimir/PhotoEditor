-- Preserve skip decisions from both defect and creative candidate reviews.
-- Skipped items pass the technical quality epoch but are not adopted.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.adopt_apply_results(p_tenant uuid,p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;r photo.runs%ROWTYPE;n photo.apply_nodes%ROWTYPE;t photo.tasks%ROWTYPE;
 tr photo.task_results%ROWTYPE;a photo.artifacts%ROWTYPE;src photo.asset_versions%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 producer photo.tasks%ROWTYPE;producer_result photo.task_results%ROWTYPE;
 new_version uuid;position_value bigint;adopted int:=0;skipped int:=0;source_phase text;
BEGIN
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF b.run_id IS NULL OR r.id IS NULL OR b.phase<>'adopt' OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp()
  OR r.cancel_requested OR r.status NOT IN ('queued','running') OR NOT r.graph_sealed THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.apply_nodes qn JOIN photo.tasks qt ON qt.tenant_id=qn.tenant_id AND qt.id=qn.id AND qt.state='succeeded' AND qt.canonical_result_id IS NOT NULL
  JOIN photo.task_results qr ON qr.tenant_id=qt.tenant_id AND qr.id=qt.canonical_result_id AND qr.task_id=qt.id
  JOIN photo.artifacts qa ON qa.tenant_id=qt.tenant_id AND qa.created_by_task_id=qt.id AND qa.produced_attempt=qr.attempt AND qa.produced_fence=qr.fencing_token
   AND qa.output_role='quality_report' AND qa.state='ready' AND qr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',qa.id,'sha256',qa.sha256))
  WHERE qn.tenant_id=p_tenant AND qn.run_id=p_run AND qn.phase='quality')<>b.item_count THEN RETURN false;END IF;
 source_phase:=CASE WHEN b.graph_version=4 THEN 'quality' ELSE 'local' END;
 IF (SELECT count(*) FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND phase=source_phase)<>b.item_count THEN RETURN false;END IF;
 FOR n IN SELECT * FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND phase=source_phase ORDER BY ordinal LOOP
  IF b.graph_version=4 AND EXISTS(SELECT FROM photo.apply_nodes proposal JOIN photo.reviews v ON v.tenant_id=proposal.tenant_id AND v.task_id=proposal.id
    WHERE proposal.tenant_id=p_tenant AND proposal.run_id=p_run AND proposal.ordinal=n.ordinal AND proposal.source_item_id=n.source_item_id
     AND v.state='decided' AND v.decision_route='skip' AND (proposal.phase='defects_propose' AND v.kind='defects'
       OR proposal.phase IN('background','inpaint','generate','crop') AND v.kind IN('background_generation','inpaint','generation','composition'))) THEN
   skipped:=skipped+1;CONTINUE;
  END IF;
  IF b.graph_version=4 THEN
   SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=n.id FOR UPDATE;
   IF t.id IS NULL OR t.state<>'succeeded' OR t.canonical_result_id IS NULL OR n.operation<>'quality.check'
    OR n.params->>'target_kind'<>'working' OR n.params->>'stage'<>'master'
    OR NOT(t.inputs @> jsonb_build_array(n.params->'target')) THEN RETURN false;END IF;
   SELECT pt.* INTO producer FROM photo.tasks pt JOIN photo.apply_nodes pn ON pn.tenant_id=pt.tenant_id AND pn.id=pt.id AND pn.run_id=p_run
    JOIN photo.apply_v4_stages ps ON ps.tenant_id=pn.tenant_id AND ps.run_id=pn.run_id AND ps.phase=pn.phase
    JOIN photo.apply_v4_stages qs ON qs.tenant_id=n.tenant_id AND qs.run_id=n.run_id AND qs.phase='quality'
    WHERE pt.tenant_id=p_tenant AND pt.run_id=p_run AND pt.state='succeeded' AND pt.canonical_result_id IS NOT NULL
     AND pn.source_item_id=n.source_item_id AND pn.ordinal=n.ordinal AND ps.stage_index<qs.stage_index
     AND EXISTS(SELECT FROM photo.artifacts candidate WHERE candidate.tenant_id=pt.tenant_id AND candidate.created_by_task_id=pt.id
      AND candidate.id=(n.params->'target'->>'artifact_id')::uuid AND candidate.sha256=n.params->'target'->>'sha256' AND candidate.output_role='working_image')
    ORDER BY ps.stage_index DESC LIMIT 1;
   IF producer.id IS NULL THEN
    SELECT rt.* INTO producer FROM photo.apply_nodes proposal
     JOIN photo.reviews v ON v.tenant_id=proposal.tenant_id AND v.task_id=proposal.id AND v.kind='defects' AND v.state='decided' AND v.decision_route='repair'
     JOIN photo.defect_branches d ON d.tenant_id=v.tenant_id AND d.review_id=v.id AND d.run_id=proposal.run_id
     JOIN photo.tasks rt ON rt.tenant_id=d.tenant_id AND rt.id=d.task_id AND rt.run_id=p_run AND rt.operation='defects.repair' AND rt.state='succeeded' AND rt.canonical_result_id IS NOT NULL
     WHERE proposal.tenant_id=p_tenant AND proposal.run_id=p_run AND proposal.phase='defects_propose'
      AND proposal.ordinal=n.ordinal AND proposal.source_item_id=n.source_item_id
      AND EXISTS(SELECT FROM photo.artifacts candidate WHERE candidate.tenant_id=rt.tenant_id AND candidate.created_by_task_id=rt.id
       AND candidate.id=(n.params->'target'->>'artifact_id')::uuid AND candidate.sha256=n.params->'target'->>'sha256' AND candidate.output_role='working_image');
   END IF;
   IF producer.id IS NULL THEN RETURN false;END IF;
   SELECT * INTO producer_result FROM photo.task_results WHERE tenant_id=p_tenant AND id=producer.canonical_result_id AND task_id=producer.id;
   SELECT x.* INTO a FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND x.id=(n.params->'target'->>'artifact_id')::uuid
    AND x.sha256=n.params->'target'->>'sha256' AND x.created_by_task_id=producer.id AND x.produced_attempt=producer_result.attempt
    AND x.produced_fence=producer_result.fencing_token AND x.output_role='working_image' AND x.state='ready' AND x.object_version IS NOT NULL
    AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp())
    AND producer_result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',x.id,'sha256',x.sha256));
  ELSE
   SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=n.id FOR UPDATE;
   IF t.id IS NULL OR t.state<>'succeeded' OR t.canonical_result_id IS NULL THEN RETURN false;END IF;
   SELECT * INTO tr FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id;
   SELECT x.* INTO a FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND x.created_by_task_id=t.id AND x.produced_attempt=tr.attempt
    AND x.produced_fence=tr.fencing_token AND x.output_role='working_image' AND x.state='ready' AND x.object_version IS NOT NULL
    AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) AND tr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',x.id,'sha256',x.sha256));
  END IF;
  IF a.id IS NULL THEN RETURN false;END IF;
  SELECT * INTO src FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=n.source_version_id AND asset_id=n.source_asset_id;
  IF src.id IS NULL THEN RETURN false;END IF;
  PERFORM id FROM photo.assets WHERE tenant_id=p_tenant AND id=n.source_asset_id AND deleted_at IS NULL AND upload_state='verified' FOR UPDATE;
  IF NOT FOUND THEN RETURN false;END IF;
  new_version:=gen_random_uuid();
  INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,parent_asset_version_id,created_by_run_id,content_hash,metadata,review_state,version_no)
   VALUES(new_version,p_tenant,n.source_asset_id,a.id,n.source_version_id,p_run,a.sha256,src.metadata,'pending_review',(SELECT coalesce(max(version_no),0)+1 FROM photo.asset_versions WHERE tenant_id=p_tenant AND asset_id=n.source_asset_id));
  INSERT INTO photo.asset_version_sources(tenant_id,version_id,source_version_id,role,ordinal) VALUES(p_tenant,new_version,n.source_version_id,'apply_source',0);
  SELECT (i.payload->>'position')::bigint INTO position_value FROM photo.runs rr JOIN photo.snapshot_items i ON i.tenant_id=rr.tenant_id AND i.snapshot_id=rr.input_snapshot_id
   WHERE rr.tenant_id=p_tenant AND rr.id=p_run AND i.item_id=n.source_item_id AND i.ordinal=n.ordinal;
  IF position_value IS NULL THEN RETURN false;END IF;
  INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position,visibility,moderation_state,rating,flag,tags,created_by_run_id,source_item_id)
   SELECT p_tenant,r.output_collection_id,n.source_asset_id,new_version,position_value,'hidden','pending_review',i.rating,i.flag,i.tags,p_run,i.id
   FROM photo.collection_items i WHERE i.tenant_id=p_tenant AND i.collection_id=b.source_collection_id AND i.id=n.source_item_id AND i.asset_id=n.source_asset_id AND i.asset_version_id=n.source_version_id;
  IF NOT FOUND THEN RETURN false;END IF;adopted:=adopted+1;a:=NULL;producer:=NULL;producer_result:=NULL;
 END LOOP;
 IF adopted+skipped<>b.item_count THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run AND state='reserved' FOR UPDATE;
 IF FOUND THEN
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
  IF NOT FOUND THEN RAISE EXCEPTION 'RUN_RESERVATION_INCONSISTENT' USING ERRCODE='23514';END IF;
  UPDATE photo.quota_reservations SET bytes=0,state='consumed' WHERE id=q.id;
 END IF;
 UPDATE photo.apply_builds SET phase='complete',lease_token=NULL,lease_until=NULL,last_error=NULL,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run;
 UPDATE photo.runs SET status='completed',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=p_tenant AND id=r.output_collection_id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_apply_results(uuid,uuid,uuid) OWNER TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
