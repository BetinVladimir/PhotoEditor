-- Complete the reviewed sensor-dust Apply v4 continuation.
-- +goose Up
ALTER TABLE photo.apply_v4_stages DROP CONSTRAINT apply_v4_stages_expected_nodes_check;
ALTER TABLE photo.apply_v4_stages ADD CONSTRAINT apply_v4_stages_expected_nodes_check CHECK(expected_nodes BETWEEN 0 AND 1000);
GRANT UPDATE(expected_nodes) ON photo.apply_v4_stages TO photo_fn_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.set_apply_v4_group_count(p_tenant uuid,p_run uuid,p_token uuid,p_count int) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;s photo.apply_v4_stages%ROWTYPE;BEGIN
 IF p_count NOT BETWEEN 0 AND 1000 THEN RETURN false;END IF;SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR b.graph_version<>4 OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() OR b.stage_index IS NULL THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index FOR UPDATE;
 IF NOT FOUND OR s.phase<>b.phase OR s.cardinality<>'group' OR s.expected_nodes IS NOT NULL AND s.expected_nodes<>p_count THEN RETURN false;END IF;
 UPDATE photo.apply_v4_stages SET expected_nodes=p_count WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index AND expected_nodes IS NULL;RETURN true;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.set_apply_v4_group_count(uuid,uuid,uuid,int) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.set_apply_v4_group_count(uuid,uuid,uuid,int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.set_apply_v4_group_count(uuid,uuid,uuid,int) TO photo_scheduler;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.register_apply_v4_plan(p_tenant uuid,p_run uuid,p_token uuid,p_hash text,p_plan jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;entry jsonb;i int:=0;old_hash text;
 allowed constant text[]:=ARRAY['mask.person','color.stats','style.resolve','image.denoise','depth.predict','pose.detect','geometry.body','mask.transform','image.color','series.measure','series.balance','image.local','face.detect','face.landmarks','face.parse','mask.face_skin','portrait.beauty','portrait.detail','defects.propose','defects.repair','background.compose','background.blur','image.inpaint','image.generate','composition.structure','composition.plan','geometry.crop','sensor.dust','sensor.dust_repair','image.upscale','quality.check'];
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
        OR (previous_stage.phase IN('background_review','inpaint_review','generation_review','composition_review') AND (SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND n.phase=(SELECT q.phase FROM photo.apply_v4_stages q WHERE q.tenant_id=b.tenant_id AND q.run_id=b.run_id AND q.stage_index=b.stage_index-2) AND v.state='decided')=b.item_count)
        OR (previous_stage.phase='dust_review' AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id LEFT JOIN photo.reviews v ON v.tenant_id=n.tenant_id AND v.task_id=n.id AND v.kind='sensor_dust' WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=(SELECT q.phase FROM photo.apply_v4_stages q WHERE q.tenant_id=b.tenant_id AND q.run_id=b.run_id AND q.stage_index=b.stage_index-2) AND t.state='succeeded' AND (NOT z.review_required OR v.state='decided'))=(SELECT q.expected_nodes FROM photo.apply_v4_stages q WHERE q.tenant_id=b.tenant_id AND q.run_id=b.run_id AND q.stage_index=b.stage_index-2))))
    OR (b.stage_index>0 AND current_stage.cardinality='review' AND r.status IN('running','waiting_review') AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase)=CASE previous_stage.cardinality WHEN 'item' THEN b.item_count WHEN 'aggregate' THEN 1 WHEN 'group' THEN previous_stage.expected_nodes ELSE -1 END
       AND (current_stage.phase='defects_review' AND ((SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND v.kind='defects' AND n.phase=previous_stage.phase)<b.item_count OR ((SELECT count(*) FROM photo.defect_branches d WHERE d.tenant_id=b.tenant_id AND d.run_id=b.run_id)=b.item_count AND (EXISTS(SELECT FROM photo.graph_expansions e WHERE e.tenant_id=b.tenant_id AND e.run_id=b.run_id AND e.state='materialized') OR NOT EXISTS(SELECT FROM photo.defect_branches d JOIN photo.reviews v ON v.tenant_id=d.tenant_id AND v.id=d.review_id WHERE d.tenant_id=b.tenant_id AND d.run_id=b.run_id AND v.decision_route='repair' AND EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=d.tenant_id AND t.id=d.task_id AND t.state<>'succeeded'))))) OR current_stage.phase IN('background_review','inpaint_review','generation_review','composition_review') AND ((SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND n.phase=previous_stage.phase)<b.item_count OR NOT EXISTS(SELECT FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND n.phase=previous_stage.phase AND v.state<>'decided'))
        OR current_stage.phase='dust_review' AND previous_stage.cardinality='group' AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase AND t.state='succeeded')=previous_stage.expected_nodes AND ((SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND v.kind='sensor_dust' AND n.phase=previous_stage.phase)<(SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase AND z.review_required) OR NOT EXISTS(SELECT FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND v.kind='sensor_dust' AND n.phase=previous_stage.phase AND v.state<>'decided')))))
  ORDER BY b.updated_at,b.run_id FOR UPDATE OF b SKIP LOCKED LIMIT 1
 ),opened AS (UPDATE photo.runs r SET graph_sealed=false,status=CASE WHEN r.status='waiting_review' THEN 'running' ELSE r.status END,revision=revision+1 FROM candidate c JOIN photo.apply_builds b ON b.tenant_id=c.tenant_id AND b.run_id=c.run_id WHERE r.tenant_id=c.tenant_id AND r.id=c.run_id AND r.graph_sealed AND b.phase<>'adopt' RETURNING r.id),
 claimed AS (UPDATE photo.apply_builds b SET lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp() FROM candidate c WHERE b.tenant_id=c.tenant_id AND b.run_id=c.run_id AND (b.phase='adopt' OR EXISTS(SELECT FROM opened o WHERE o.id=b.run_id) OR EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=b.tenant_id AND r.id=b.run_id AND NOT r.graph_sealed)) RETURNING b.*)
 SELECT c.tenant_id,c.run_id,c.source_collection_id,c.style_version_id,c.phase,c.item_count,c.lease_token,c.lease_until,c.graph_version,c.stage_index FROM claimed c;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_apply_build_versioned() OWNER TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.open_apply_v4_dust_reviews(p_tenant uuid,p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;s photo.apply_v4_stages%ROWTYPE;previous photo.apply_v4_stages%ROWTYPE;n photo.apply_nodes%ROWTYPE;z photo.task_results%ROWTYPE;review_id uuid;opened int:=0;
BEGIN
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO s FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index;
 SELECT * INTO previous FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index-1;
 IF b.run_id IS NULL OR b.graph_version<>4 OR b.phase<>'dust_review' OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp()
  OR s.cardinality<>'review' OR previous.operation<>'sensor.dust' OR previous.cardinality<>'group' OR previous.expected_nodes IS NULL THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.apply_nodes pn JOIN photo.tasks pt ON pt.tenant_id=pn.tenant_id AND pt.id=pn.id
     WHERE pn.tenant_id=p_tenant AND pn.run_id=p_run AND pn.phase=previous.phase AND pt.state='succeeded' AND pt.canonical_result_id IS NOT NULL)<>previous.expected_nodes THEN RETURN false;END IF;
 FOR n IN SELECT * FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND phase=previous.phase ORDER BY ordinal LOOP
  SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=(SELECT canonical_result_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=n.id) AND task_id=n.id;
  IF z.id IS NULL THEN RETURN false;END IF;
  IF z.review_required THEN
   review_id:=photo.create_dust_review(p_tenant,p_run,n.id,(n.params->'frames'->0->>'artifact_id')::uuid);
   IF review_id IS NULL THEN RETURN false;END IF;opened:=opened+1;
  END IF;
 END LOOP;
 IF (SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes pn ON pn.tenant_id=v.tenant_id AND pn.id=v.task_id
     WHERE v.tenant_id=p_tenant AND v.run_id=p_run AND v.kind='sensor_dust' AND pn.phase=previous.phase)<>opened THEN RETURN false;END IF;
 UPDATE photo.apply_builds SET lease_token=NULL,lease_until=NULL,last_error=NULL,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run AND lease_token=p_token;
 IF NOT FOUND THEN RETURN false;END IF;
 IF opened>0 THEN UPDATE photo.runs SET status='waiting_review',graph_sealed=false WHERE tenant_id=p_tenant AND id=p_run AND NOT cancel_requested;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation THEN RETURN false;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.open_apply_v4_dust_reviews(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.open_apply_v4_dust_reviews(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.open_apply_v4_dust_reviews(uuid,uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.process_apply_v4_dust_reviews(p_tenant uuid,p_run uuid,p_token uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;s photo.apply_v4_stages%ROWTYPE;previous photo.apply_v4_stages%ROWTYPE;next_stage photo.apply_v4_stages%ROWTYPE;required_count int;review_count int;
BEGIN
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO s FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index;
 SELECT * INTO previous FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index-1;
 IF b.run_id IS NULL OR b.graph_version<>4 OR b.phase<>'dust_review' OR s.cardinality<>'review' OR previous.operation<>'sensor.dust'
  OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() THEN RETURN 'rejected';END IF;
 SELECT count(*) INTO required_count FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id
  JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
  WHERE n.tenant_id=p_tenant AND n.run_id=p_run AND n.phase=previous.phase AND t.state='succeeded' AND z.review_required;
 SELECT count(*) INTO review_count FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id
  WHERE v.tenant_id=p_tenant AND v.run_id=p_run AND v.kind='sensor_dust' AND n.phase=previous.phase;
 IF review_count<required_count THEN IF photo.open_apply_v4_dust_reviews(p_tenant,p_run,p_token) THEN RETURN 'opened';END IF;RETURN 'rejected';END IF;
 IF review_count<>required_count OR EXISTS(SELECT FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=p_tenant AND v.run_id=p_run AND v.kind='sensor_dust' AND n.phase=previous.phase AND v.state<>'decided') THEN RETURN 'rejected';END IF;
 SELECT * INTO next_stage FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index+1;
 IF next_stage.phase<>'dust_repair' OR next_stage.operation<>'sensor.dust_repair' OR next_stage.cardinality<>'item' THEN RETURN 'rejected';END IF;
 UPDATE photo.apply_builds SET stage_index=stage_index+1,phase=next_stage.phase,lease_token=NULL,lease_until=NULL,last_error=NULL,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run AND lease_token=p_token;
 IF NOT FOUND THEN RETURN 'rejected';END IF;
 UPDATE photo.runs SET status='running',graph_sealed=true,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run AND NOT cancel_requested;
 RETURN CASE WHEN FOUND THEN 'advanced' ELSE 'rejected' END;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.process_apply_v4_dust_reviews(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.process_apply_v4_dust_reviews(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.process_apply_v4_dust_reviews(uuid,uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.apply_v4_dust_decisions(p_tenant uuid,p_run uuid,p_token uuid)
RETURNS TABLE(group_ordinal int,task_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text,selection jsonb,route text)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT n.ordinal,n.id,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type,
  coalesce(v.selection,jsonb_build_object('version','sensor_dust_review_v1','proposal',jsonb_build_object('artifact_id',a.id,'sha256',a.sha256),'base',n.params->'frames'->0,'revision',1,'accepted_candidate_ids','[]'::jsonb,'rejected_candidate_ids','[]'::jsonb)),
  coalesce(v.decision_route,'base')
 FROM photo.apply_builds b JOIN photo.apply_v4_stages current_stage ON current_stage.tenant_id=b.tenant_id AND current_stage.run_id=b.run_id AND current_stage.stage_index=b.stage_index
 JOIN photo.apply_nodes n ON n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase='dust_detect'
 JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.state='ready' AND a.output_role='dust_candidates'
  AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
 LEFT JOIN photo.reviews v ON v.tenant_id=n.tenant_id AND v.run_id=n.run_id AND v.task_id=n.id AND v.kind='sensor_dust' AND v.state='decided'
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.graph_version=4 AND b.lease_token=p_token AND b.lease_until>=clock_timestamp()
  AND current_stage.phase='dust_repair' AND (NOT z.review_required OR v.id IS NOT NULL)
 ORDER BY n.ordinal
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.apply_v4_dust_decisions(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v4_dust_decisions(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.apply_v4_dust_decisions(uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
