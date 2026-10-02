-- Every apply master passes the same technical acceptance operation before it
-- can be adopted.  The gate is a separate sealed epoch and is bound to the
-- canonical image.local output for the same immutable snapshot ordinal.
-- +goose Up
ALTER TABLE photo.apply_builds DROP CONSTRAINT apply_builds_phase_check;
ALTER TABLE photo.apply_builds ADD CONSTRAINT apply_builds_phase_check CHECK(phase IN ('mask','stats','resolve','color','local','quality','adopt','complete','failed'));
ALTER TABLE photo.apply_nodes DROP CONSTRAINT apply_nodes_phase_check;
ALTER TABLE photo.apply_nodes ADD CONSTRAINT apply_nodes_phase_check CHECK(phase IN ('mask','stats','resolve','color','local','quality'));
ALTER TABLE photo.apply_nodes DROP CONSTRAINT apply_nodes_check;
ALTER TABLE photo.apply_nodes ADD CONSTRAINT apply_nodes_operation_check CHECK(operation=CASE phase WHEN 'mask' THEN 'mask.person' WHEN 'stats' THEN 'color.stats' WHEN 'resolve' THEN 'style.resolve' WHEN 'color' THEN 'image.color' WHEN 'local' THEN 'image.local' ELSE 'quality.check' END);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_apply_build()
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
    OR (b.phase='quality' AND (NOT r.graph_sealed OR (SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=b.tenant_id AND t.run_id=b.run_id AND t.operation='image.local' AND t.state='succeeded')=b.item_count))
    OR (b.phase='adopt' AND r.graph_sealed AND (SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=b.tenant_id AND t.run_id=b.run_id AND t.operation='quality.check' AND t.state='succeeded')=b.item_count))
  ORDER BY b.updated_at,b.run_id FOR UPDATE OF b SKIP LOCKED LIMIT 1
 ), opened AS (
  UPDATE photo.runs r SET graph_sealed=false,revision=revision+1 FROM candidate c JOIN photo.apply_builds b ON b.tenant_id=c.tenant_id AND b.run_id=c.run_id
  WHERE r.tenant_id=c.tenant_id AND r.id=c.run_id AND r.graph_sealed AND b.phase IN ('stats','resolve','color','local','quality') RETURNING r.id
 ), claimed AS (
  UPDATE photo.apply_builds b SET lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp()
  FROM candidate c WHERE b.tenant_id=c.tenant_id AND b.run_id=c.run_id
   AND (b.phase IN ('mask','adopt') OR EXISTS(SELECT FROM opened o WHERE o.id=b.run_id)
    OR EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=b.tenant_id AND r.id=b.run_id AND NOT r.graph_sealed)) RETURNING b.*
 ) SELECT c.tenant_id,c.run_id,c.source_collection_id,c.style_version_id,c.phase,c.item_count,c.lease_token,c.lease_until FROM claimed c;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.advance_apply_build(p_run uuid,p_token uuid,p_from text,p_to text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n int;expected int;op text;
BEGIN
 IF p_to IS DISTINCT FROM (CASE p_from WHEN 'mask' THEN 'stats' WHEN 'stats' THEN 'resolve' WHEN 'resolve' THEN 'color' WHEN 'color' THEN 'local' WHEN 'local' THEN 'quality' WHEN 'quality' THEN 'adopt' WHEN 'adopt' THEN 'complete' END) THEN RETURN false;END IF;
 IF p_from='adopt' THEN RETURN false;END IF;
 op:=CASE p_from WHEN 'mask' THEN 'mask.person' WHEN 'stats' THEN 'color.stats' WHEN 'resolve' THEN 'style.resolve' WHEN 'color' THEN 'image.color' WHEN 'local' THEN 'image.local' ELSE 'quality.check' END;
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.apply_inputs(p_tenant uuid,p_run uuid,p_token uuid)
RETURNS TABLE(ordinal bigint,item_id uuid,asset_id uuid,asset_version_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text,payload jsonb,config_snapshot jsonb,style_artifact_id uuid,style_sha256 text,style_bytes bigint)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT i.ordinal,i.item_id,v.asset_id,i.asset_version_id,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type,i.payload,r.config_snapshot,sa.id,sa.sha256,sa.bytes
 FROM photo.apply_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 JOIN photo.snapshot_items i ON i.tenant_id=r.tenant_id AND i.snapshot_id=r.input_snapshot_id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.master_artifact_id=i.artifact_id AND v.content_hash=i.sha256
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 JOIN photo.style_versions sv ON sv.tenant_id=b.tenant_id AND sv.id=b.style_version_id AND sv.approval_status='accepted'
 JOIN photo.artifacts sa ON sa.tenant_id=sv.tenant_id AND sa.id=sv.export_artifact_id AND sa.sha256=sv.config_hash AND sa.state='ready' AND sa.object_version IS NOT NULL
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>=clock_timestamp() AND b.phase IN ('mask','stats','resolve','color','local','quality') ORDER BY i.ordinal
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.apply_stage_outputs(p_tenant uuid,p_run uuid,p_token uuid,p_operation text,p_role text)
RETURNS TABLE(ordinal int,item_id uuid,task_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT n.ordinal,n.source_item_id,t.id,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type FROM photo.apply_builds b
 JOIN photo.apply_nodes n ON n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.operation=p_operation
 JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL
 JOIN photo.task_results tr ON tr.tenant_id=t.tenant_id AND tr.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.produced_attempt=tr.attempt AND a.produced_fence=tr.fencing_token
  AND a.output_role=p_role AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>=clock_timestamp()
  AND ((b.phase IN ('stats','resolve','color','local') AND p_operation='mask.person' AND p_role='person_mask_ref')
   OR (b.phase='resolve' AND p_operation='color.stats' AND p_role='stats')
   OR (b.phase IN ('color','local') AND p_operation='style.resolve' AND p_role='resolved_style')
   OR (b.phase='local' AND p_operation='image.color' AND p_role='working_image')
   OR (b.phase='quality' AND p_operation='image.local' AND p_role='working_image')
   OR (b.phase='adopt' AND p_operation='quality.check' AND p_role='quality_report'))
  AND tr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)) ORDER BY n.ordinal
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.register_apply_quality_node(p_tenant uuid,p_run uuid,p_token uuid,p_node jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;old photo.apply_nodes%ROWTYPE;local_node photo.apply_nodes%ROWTYPE;local_task photo.tasks%ROWTYPE;result photo.task_results%ROWTYPE;target photo.artifacts%ROWTYPE;source photo.artifacts%ROWTYPE;
BEGIN
 IF p_node IS NULL OR jsonb_typeof(p_node)<>'object' OR octet_length(p_node::text)>1048576
 OR (p_node-'id'-'phase'-'ordinal'-'source_item_id'-'source_asset_id'-'source_version_id'-'node_key'-'operation'-'inputs'-'prerequisites'-'params_sha256'-'params')<>'{}'
 OR p_node->>'phase'<>'quality' OR p_node->>'operation'<>'quality.check' OR p_node->>'params_sha256' !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_node->'inputs')<>'array' OR jsonb_typeof(p_node->'prerequisites')<>'array' OR jsonb_typeof(p_node->'params')<>'object' THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF b.run_id IS NULL OR b.phase<>'quality' OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() OR (p_node->>'ordinal')::int NOT BETWEEN 0 AND b.item_count-1 THEN RETURN false;END IF;
 SELECT * INTO local_node FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND phase='local' AND ordinal=(p_node->>'ordinal')::int;
 SELECT * INTO local_task FROM photo.tasks WHERE tenant_id=p_tenant AND id=local_node.id AND state='succeeded';
 SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=local_task.canonical_result_id AND task_id=local_task.id;
 SELECT a.* INTO target FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(p_node->'params'->'target'->>'artifact_id')::uuid AND a.sha256=p_node->'params'->'target'->>'sha256' AND a.created_by_task_id=local_task.id AND a.produced_attempt=result.attempt AND a.produced_fence=result.fencing_token AND a.output_role='working_image' AND a.state='ready' AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256));
 SELECT a.* INTO source FROM photo.asset_versions v JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.master_artifact_id AND a.sha256=v.content_hash WHERE v.tenant_id=p_tenant AND v.id=(p_node->>'source_version_id')::uuid AND v.asset_id=(p_node->>'source_asset_id')::uuid AND a.id=(p_node->'params'->'baseline'->>'artifact_id')::uuid AND a.sha256=p_node->'params'->'baseline'->>'sha256' AND a.state='ready';
 IF local_node.id IS NULL OR local_task.id IS NULL OR result.id IS NULL OR target.id IS NULL OR source.id IS NULL
 OR local_node.source_item_id<>(p_node->>'source_item_id')::uuid OR (p_node->'prerequisites')<>jsonb_build_array(local_task.id)
 OR p_node->'params'->>'target_kind'<>'working' OR p_node->'params'->>'stage'<>'master' OR p_node->'params'->>'threshold_profile_id'<>'master_v1'
 OR p_node->'params'->'protected'<>'[]'::jsonb OR p_node->'params'->'seams'<>'[]'::jsonb THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_node->>'id')::uuid;
 IF FOUND THEN RETURN to_jsonb(old)-'tenant_id'-'run_id'=p_node;END IF;
 IF EXISTS(SELECT FROM photo.apply_nodes WHERE id=(p_node->>'id')::uuid) THEN RETURN false;END IF;
 INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,source_asset_id,source_version_id,node_key,operation,inputs,prerequisites,params_sha256,params)
 VALUES((p_node->>'id')::uuid,p_tenant,p_run,'quality',(p_node->>'ordinal')::int,(p_node->>'source_item_id')::uuid,(p_node->>'source_asset_id')::uuid,(p_node->>'source_version_id')::uuid,p_node->>'node_key','quality.check',p_node->'inputs',ARRAY[local_task.id],p_node->>'params_sha256',p_node->'params');RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_apply_quality_node(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_apply_quality_node(uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_apply_quality_node(uuid,uuid,uuid,jsonb) TO photo_scheduler;

-- Keep adoption in the existing transaction, but refuse it unless each source
-- ordinal has a canonical fenced quality report from the exact quality node.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.adopt_apply_results(p_tenant uuid,p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;r photo.runs%ROWTYPE;n photo.apply_nodes%ROWTYPE;t photo.tasks%ROWTYPE;
 tr photo.task_results%ROWTYPE;a photo.artifacts%ROWTYPE;src photo.asset_versions%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 new_version uuid;position_value bigint;adopted int:=0;
BEGIN
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF b.run_id IS NULL OR r.id IS NULL OR b.phase<>'adopt' OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp()
  OR r.cancel_requested OR r.status NOT IN ('queued','running') OR NOT r.graph_sealed THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND phase='local')<>b.item_count
  OR (SELECT count(*) FROM photo.apply_nodes qn JOIN photo.tasks qt ON qt.tenant_id=qn.tenant_id AND qt.id=qn.id AND qt.state='succeeded' AND qt.canonical_result_id IS NOT NULL JOIN photo.task_results qr ON qr.tenant_id=qt.tenant_id AND qr.id=qt.canonical_result_id JOIN photo.artifacts qa ON qa.tenant_id=qt.tenant_id AND qa.created_by_task_id=qt.id AND qa.produced_attempt=qr.attempt AND qa.produced_fence=qr.fencing_token AND qa.output_role='quality_report' AND qa.state='ready' AND qr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',qa.id,'sha256',qa.sha256)) WHERE qn.tenant_id=p_tenant AND qn.run_id=p_run AND qn.phase='quality')<>b.item_count THEN RETURN false;END IF;
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

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
