-- Durable variable-cardinality stages for collection algorithms such as sensor dust.
-- +goose Up
ALTER TABLE photo.apply_v4_stages DROP CONSTRAINT apply_v4_stages_cardinality_check;
ALTER TABLE photo.apply_v4_stages ADD CONSTRAINT apply_v4_stages_cardinality_check CHECK(cardinality IN('item','aggregate','group','review'));
ALTER TABLE photo.apply_v4_stages ADD COLUMN expected_nodes int CHECK(expected_nodes BETWEEN 1 AND 1000);
ALTER TABLE photo.apply_v4_stages ADD CONSTRAINT apply_v4_group_expected CHECK(cardinality='group' OR expected_nodes IS NULL);

-- +goose StatementBegin
CREATE FUNCTION photo.set_apply_v4_group_count(p_tenant uuid,p_run uuid,p_token uuid,p_count int) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;s photo.apply_v4_stages%ROWTYPE;
BEGIN
 IF p_count NOT BETWEEN 1 AND 1000 THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR b.graph_version<>4 OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() OR b.stage_index IS NULL THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index FOR UPDATE;
 IF NOT FOUND OR s.phase<>b.phase OR s.cardinality<>'group' OR s.expected_nodes IS NOT NULL AND s.expected_nodes<>p_count THEN RETURN false;END IF;
 UPDATE photo.apply_v4_stages SET expected_nodes=p_count WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index AND expected_nodes IS NULL;
 RETURN true;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.set_apply_v4_group_count(uuid,uuid,uuid,int) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.set_apply_v4_group_count(uuid,uuid,uuid,int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.set_apply_v4_group_count(uuid,uuid,uuid,int) TO photo_scheduler;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.register_apply_v4_plan(p_tenant uuid,p_run uuid,p_token uuid,p_hash text,p_plan jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;entry jsonb;i int:=0;old_hash text;
 allowed constant text[]:=ARRAY['mask.person','color.stats','style.resolve','image.denoise','depth.predict','pose.detect','geometry.body','mask.transform','image.color','series.measure','series.balance','image.local','face.detect','face.landmarks','face.parse','mask.face_skin','portrait.beauty','portrait.detail','defects.propose','defects.repair','background.compose','background.blur','image.inpaint','image.generate','composition.structure','composition.plan','geometry.crop','sensor.dust','image.upscale','quality.check'];
BEGIN
 IF p_hash !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_plan)<>'array' OR jsonb_array_length(p_plan) NOT BETWEEN 4 AND 64 THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR b.graph_version<>4 OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() THEN RETURN false;END IF;
 SELECT min(plan_hash) INTO old_hash FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run;
 IF old_hash IS NOT NULL THEN RETURN old_hash=p_hash
  AND NOT EXISTS(SELECT FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND plan_hash<>p_hash)
  AND (SELECT count(*) FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run)=jsonb_array_length(p_plan)
  AND NOT EXISTS(SELECT FROM jsonb_array_elements(p_plan) WITH ORDINALITY x(value,ord)
   LEFT JOIN photo.apply_v4_stages s ON s.tenant_id=p_tenant AND s.run_id=p_run AND s.stage_index=x.ord-1
   WHERE s.stage_index IS NULL OR s.phase<>x.value->>'phase' OR coalesce(s.operation,'')<>coalesce(x.value->>'operation','')
    OR coalesce(s.output_role,'')<>coalesce(x.value->>'output_role','') OR s.cardinality<>x.value->>'cardinality');END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_plan) LOOP
  IF (entry-'phase'-'operation'-'output_role'-'cardinality')<>'{}' OR entry->>'phase' !~ '^[a-z][a-z0-9_]{0,63}$' OR entry->>'cardinality' NOT IN('item','aggregate','group','review') THEN RETURN false;END IF;
  IF entry->>'cardinality'='review' THEN IF coalesce(entry->>'operation','')<>'' OR coalesce(entry->>'output_role','')<>'' THEN RETURN false;END IF;
  ELSIF NOT ((entry->>'operation')=ANY(allowed)) OR NOT EXISTS(SELECT FROM photo.operation_registry o WHERE o.operation=entry->>'operation' AND o.version='1') OR coalesce(entry->>'output_role','')='' THEN RETURN false;END IF;
  INSERT INTO photo.apply_v4_stages(tenant_id,run_id,stage_index,phase,operation,output_role,cardinality,plan_hash)
  VALUES(p_tenant,p_run,i,entry->>'phase',NULLIF(entry->>'operation',''),NULLIF(entry->>'output_role',''),entry->>'cardinality',p_hash);i:=i+1;
 END LOOP;
 IF NOT EXISTS(SELECT FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=0 AND phase IN('mask','resolve'))
 OR NOT EXISTS(SELECT FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=i-1 AND phase='quality') THEN RAISE EXCEPTION 'INVALID_APPLY_V4_BOUNDARIES' USING ERRCODE='23514';END IF;
 UPDATE photo.apply_builds SET stage_index=0,phase=(p_plan->0->>'phase'),updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run AND graph_version=4;
 RETURN true;
EXCEPTION WHEN unique_violation OR check_violation THEN RETURN false;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.register_apply_v4_node(p_tenant uuid,p_run uuid,p_token uuid,p_node jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;s photo.apply_v4_stages%ROWTYPE;old photo.apply_nodes%ROWTYPE;entry jsonb;expected_ordinal int;source_ordinal int;
BEGIN
 IF p_node IS NULL OR jsonb_typeof(p_node)<>'object' OR octet_length(p_node::text)>1048576
 OR (p_node-'id'-'phase'-'ordinal'-'source_item_id'-'source_asset_id'-'source_version_id'-'node_key'-'operation'-'inputs'-'prerequisites'-'params_sha256'-'params')<>'{}'
 OR p_node->>'params_sha256' !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_node->'inputs')<>'array' OR jsonb_array_length(p_node->'inputs') NOT BETWEEN 1 AND 128
 OR jsonb_typeof(p_node->'prerequisites')<>'array' OR jsonb_typeof(p_node->'params')<>'object' THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR b.graph_version<>4 OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() OR b.stage_index IS NULL THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index;
 IF NOT FOUND OR s.phase<>b.phase OR s.cardinality='review' OR p_node->>'phase'<>s.phase OR (p_node->>'operation'<>s.operation AND NOT (s.phase='composition_render' AND s.operation='geometry.crop' AND p_node->>'operation'='image.outpaint')) THEN RETURN false;END IF;
 expected_ordinal:=(p_node->>'ordinal')::int;
 IF (s.cardinality='item' AND expected_ordinal NOT BETWEEN 0 AND b.item_count-1) OR (s.cardinality='aggregate' AND expected_ordinal<>0) OR (s.cardinality='group' AND (s.expected_nodes IS NULL OR expected_ordinal NOT BETWEEN 0 AND s.expected_nodes-1)) THEN RETURN false;END IF;
 source_ordinal:=CASE WHEN s.cardinality IN('aggregate','group') THEN NULL ELSE expected_ordinal END;
 IF NOT EXISTS(SELECT FROM photo.runs r JOIN photo.snapshot_items i ON i.tenant_id=r.tenant_id AND i.snapshot_id=r.input_snapshot_id
  JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND (source_ordinal IS NULL OR i.ordinal=source_ordinal) AND i.item_id=(p_node->>'source_item_id')::uuid
   AND v.asset_id=(p_node->>'source_asset_id')::uuid AND v.id=(p_node->>'source_version_id')::uuid) THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_node->'inputs') LOOP
  IF (entry-'artifact_id'-'sha256')<>'{}' OR entry->>'sha256' !~ '^[0-9a-f]{64}$'
   OR NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(entry->>'artifact_id')::uuid AND a.sha256=entry->>'sha256' AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 END LOOP;
 IF EXISTS(SELECT FROM jsonb_array_elements_text(p_node->'prerequisites') x
  LEFT JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=x::uuid AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL
  LEFT JOIN photo.apply_nodes predecessor ON predecessor.tenant_id=t.tenant_id AND predecessor.id=t.id
  LEFT JOIN photo.apply_v4_stages ps ON ps.tenant_id=predecessor.tenant_id AND ps.run_id=predecessor.run_id AND ps.phase=predecessor.phase
  WHERE t.id IS NULL OR ps.stage_index>=s.stage_index) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_node->>'id')::uuid;
 IF FOUND THEN RETURN to_jsonb(old)-'tenant_id'-'run_id'=p_node;END IF;
 IF EXISTS(SELECT FROM photo.apply_nodes WHERE id=(p_node->>'id')::uuid) THEN RETURN false;END IF;
 INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,source_asset_id,source_version_id,node_key,operation,inputs,prerequisites,params_sha256,params)
 VALUES((p_node->>'id')::uuid,p_tenant,p_run,s.phase,expected_ordinal,(p_node->>'source_item_id')::uuid,(p_node->>'source_asset_id')::uuid,(p_node->>'source_version_id')::uuid,p_node->>'node_key',p_node->>'operation',p_node->'inputs',ARRAY(SELECT jsonb_array_elements_text(p_node->'prerequisites')::uuid),p_node->>'params_sha256',p_node->'params');
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR unique_violation THEN RETURN false;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_apply_v4_node(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_apply_v4_node(uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_apply_v4_node(uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.advance_apply_v4_build(p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;s photo.apply_v4_stages%ROWTYPE;next_stage photo.apply_v4_stages%ROWTYPE;expected int;n int;
BEGIN
 SELECT * INTO b FROM photo.apply_builds WHERE run_id=p_run AND graph_version=4 AND lease_token=p_token AND lease_until>=clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR b.phase IN('adopt','complete','failed') OR b.stage_index IS NULL THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.apply_v4_stages WHERE tenant_id=b.tenant_id AND run_id=b.run_id AND stage_index=b.stage_index;
 IF NOT FOUND OR s.phase<>b.phase OR s.cardinality='review' THEN RETURN false;END IF;
 expected:=CASE s.cardinality WHEN 'item' THEN b.item_count WHEN 'aggregate' THEN 1 WHEN 'group' THEN s.expected_nodes ELSE -1 END;
 IF (SELECT count(*) FROM photo.apply_nodes WHERE tenant_id=b.tenant_id AND run_id=b.run_id AND phase=s.phase)<>expected
 OR (SELECT count(*) FROM photo.tasks t JOIN photo.apply_nodes n ON n.tenant_id=t.tenant_id AND n.id=t.id WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=s.phase AND t.state='blocked')<>expected THEN RETURN false;END IF;
 UPDATE photo.tasks t SET state='ready',next_attempt_at=clock_timestamp() FROM photo.apply_nodes n WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=s.phase AND t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='blocked';
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
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_apply_build_versioned()
RETURNS TABLE(tenant_id uuid,run_id uuid,source_collection_id uuid,style_version_id uuid,phase text,item_count int,lease_token uuid,lease_until timestamptz,graph_version int,stage_index int)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE legacy record;
BEGIN
 SELECT * INTO legacy FROM photo.claim_apply_build();
 IF FOUND THEN RETURN QUERY SELECT legacy.tenant_id,legacy.run_id,legacy.source_collection_id,legacy.style_version_id,legacy.phase,legacy.item_count,legacy.lease_token,legacy.lease_until,b.graph_version,b.stage_index FROM photo.apply_builds b WHERE b.tenant_id=legacy.tenant_id AND b.run_id=legacy.run_id AND b.lease_token=legacy.lease_token;RETURN;END IF;
 RETURN QUERY WITH candidate AS (
  SELECT b.tenant_id,b.run_id FROM photo.apply_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
  LEFT JOIN photo.apply_v4_stages current_stage ON current_stage.tenant_id=b.tenant_id AND current_stage.run_id=b.run_id AND current_stage.stage_index=b.stage_index
  LEFT JOIN photo.apply_v4_stages previous_stage ON previous_stage.tenant_id=b.tenant_id AND previous_stage.run_id=b.run_id AND previous_stage.stage_index=b.stage_index-1
  WHERE b.graph_version=4 AND b.phase NOT IN('complete','failed') AND (b.lease_until IS NULL OR b.lease_until<clock_timestamp()) AND NOT r.cancel_requested
   AND ((b.phase='adopt' AND r.status IN('queued','running') AND r.graph_sealed AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase='quality')=b.item_count)
    OR (b.stage_index>0 AND current_stage.cardinality<>'review' AND r.status IN('queued','running') AND r.graph_sealed AND
       ((previous_stage.cardinality IN('item','aggregate','group') AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase)=CASE previous_stage.cardinality WHEN 'item' THEN b.item_count WHEN 'aggregate' THEN 1 WHEN 'group' THEN previous_stage.expected_nodes ELSE -1 END)
        OR (previous_stage.phase='defects_review' AND (SELECT count(*) FROM photo.defect_branches d JOIN photo.reviews v ON v.tenant_id=d.tenant_id AND v.id=d.review_id WHERE d.tenant_id=b.tenant_id AND d.run_id=b.run_id AND v.state='decided' AND (v.decision_route<>'repair' OR EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=d.tenant_id AND t.id=d.task_id AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL)))=b.item_count)
        OR (previous_stage.phase IN('background_review','inpaint_review','generation_review','composition_review') AND (SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND n.phase=(SELECT phase FROM photo.apply_v4_stages WHERE tenant_id=b.tenant_id AND run_id=b.run_id AND stage_index=b.stage_index-2) AND v.state='decided')=b.item_count)))
    OR (b.stage_index>0 AND current_stage.cardinality='review' AND r.status IN('running','waiting_review') AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase)=b.item_count
       AND (current_stage.phase='defects_review' OR current_stage.phase IN('background_review','inpaint_review','generation_review','composition_review') AND ((SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND n.phase=previous_stage.phase)<b.item_count OR NOT EXISTS(SELECT FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND n.phase=previous_stage.phase AND v.state<>'decided')))))
  ORDER BY b.updated_at,b.run_id FOR UPDATE OF b SKIP LOCKED LIMIT 1
 ),opened AS (UPDATE photo.runs r SET graph_sealed=false,status=CASE WHEN r.status='waiting_review' THEN 'running' ELSE r.status END,revision=revision+1 FROM candidate c JOIN photo.apply_builds b ON b.tenant_id=c.tenant_id AND b.run_id=c.run_id WHERE r.tenant_id=c.tenant_id AND r.id=c.run_id AND r.graph_sealed AND b.phase<>'adopt' RETURNING r.id),
 claimed AS (UPDATE photo.apply_builds b SET lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp() FROM candidate c WHERE b.tenant_id=c.tenant_id AND b.run_id=c.run_id AND (b.phase='adopt' OR EXISTS(SELECT FROM opened o WHERE o.id=b.run_id) OR EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=b.tenant_id AND r.id=b.run_id AND NOT r.graph_sealed)) RETURNING b.*)
 SELECT c.tenant_id,c.run_id,c.source_collection_id,c.style_version_id,c.phase,c.item_count,c.lease_token,c.lease_until,c.graph_version,c.stage_index FROM claimed c;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_apply_build_versioned() OWNER TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
