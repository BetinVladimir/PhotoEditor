-- Adopt only canonical, fenced final outputs. The entire run is committed in
-- one database transaction, so a retry cannot expose a partially built result.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_apply_results(p_tenant uuid,p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;r photo.runs%ROWTYPE;n photo.apply_nodes%ROWTYPE;t photo.tasks%ROWTYPE;
 tr photo.task_results%ROWTYPE;a photo.artifacts%ROWTYPE;src photo.asset_versions%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 new_version uuid;position_value bigint;adopted int:=0;
BEGIN
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF b.run_id IS NULL OR r.id IS NULL OR b.phase<>'adopt' OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp()
  OR r.cancel_requested OR r.status NOT IN ('queued','running') OR NOT r.graph_sealed THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND phase='local')<>b.item_count THEN RETURN false;END IF;
 FOR n IN SELECT * FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND phase='local' ORDER BY ordinal LOOP
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=n.id FOR UPDATE;
  IF t.id IS NULL OR t.state<>'succeeded' OR t.canonical_result_id IS NULL THEN RETURN false;END IF;
  SELECT * INTO tr FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id;
  IF tr.id IS NULL THEN RETURN false;END IF;
  SELECT x.* INTO a FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND x.created_by_task_id=t.id AND x.produced_attempt=tr.attempt
   AND x.produced_fence=tr.fencing_token AND x.output_role='working_image' AND x.state='ready' AND x.object_version IS NOT NULL
   AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) AND tr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',x.id,'sha256',x.sha256));
  IF a.id IS NULL THEN RETURN false;END IF;
  SELECT * INTO src FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=n.source_version_id AND asset_id=n.source_asset_id;
  IF src.id IS NULL THEN RETURN false;END IF;
  PERFORM id FROM photo.assets WHERE tenant_id=p_tenant AND id=n.source_asset_id AND deleted_at IS NULL AND upload_state='verified' FOR UPDATE;
  IF NOT FOUND THEN RETURN false;END IF;
  new_version:=gen_random_uuid();
  INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,parent_asset_version_id,created_by_run_id,content_hash,metadata,review_state,version_no)
   VALUES(new_version,p_tenant,n.source_asset_id,a.id,n.source_version_id,p_run,a.sha256,src.metadata,'pending_review',(SELECT coalesce(max(version_no),0)+1 FROM photo.asset_versions WHERE tenant_id=p_tenant AND asset_id=n.source_asset_id));
  INSERT INTO photo.asset_version_sources(tenant_id,version_id,source_version_id,role,ordinal) VALUES(p_tenant,new_version,n.source_version_id,'apply_source',0);
  SELECT (i.payload->>'position')::bigint INTO position_value FROM photo.runs rr JOIN photo.snapshot_items i ON i.tenant_id=rr.tenant_id AND i.snapshot_id=rr.input_snapshot_id WHERE rr.tenant_id=p_tenant AND rr.id=p_run AND i.item_id=n.source_item_id AND i.ordinal=n.ordinal;
  IF position_value IS NULL THEN RETURN false;END IF;
  INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position,visibility,moderation_state,rating,flag,tags,created_by_run_id,source_item_id)
   SELECT p_tenant,r.output_collection_id,n.source_asset_id,new_version,position_value,'hidden','pending_review',i.rating,i.flag,i.tags,p_run,i.id
   FROM photo.collection_items i WHERE i.tenant_id=p_tenant AND i.collection_id=b.source_collection_id AND i.id=n.source_item_id AND i.asset_id=n.source_asset_id AND i.asset_version_id=n.source_version_id;
  IF NOT FOUND THEN RETURN false;END IF;adopted:=adopted+1;
 END LOOP;
 IF adopted<>b.item_count THEN RETURN false;END IF;
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
REVOKE ALL ON FUNCTION photo.adopt_apply_results(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_apply_results(uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- Forward-only production migrations. Restore a backup for destructive rollback.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
