-- Expand Analyze from the color-only path to a fenced geometry feature DAG.

-- +goose Up
ALTER TABLE photo.style_analysis_builds ADD COLUMN graph_version smallint NOT NULL DEFAULT 1 CHECK(graph_version IN (1,2));
ALTER TABLE photo.style_analysis_builds ALTER COLUMN graph_version SET DEFAULT 2;

ALTER TABLE photo.style_analysis_builds DROP CONSTRAINT style_analysis_builds_phase_check;
ALTER TABLE photo.style_analysis_builds ADD CONSTRAINT style_analysis_builds_phase_check CHECK
 (phase IN ('registration','features','body_fit','geometry','geometry_registration','color_fit','aggregate','complete','failed'));
ALTER TABLE photo.style_analysis_nodes DROP CONSTRAINT style_analysis_nodes_phase_check;
ALTER TABLE photo.style_analysis_nodes DROP CONSTRAINT style_analysis_nodes_ordinal_check;
ALTER TABLE photo.style_analysis_nodes DROP CONSTRAINT style_analysis_nodes_check;
ALTER TABLE photo.style_analysis_nodes ADD CONSTRAINT style_analysis_nodes_phase_check CHECK
 (phase IN ('registration','features','body_fit','geometry','geometry_registration','color_fit','aggregate'));
ALTER TABLE photo.style_analysis_nodes ADD CONSTRAINT style_analysis_nodes_ordinal_check CHECK(ordinal BETWEEN 0 AND 127);
ALTER TABLE photo.style_analysis_nodes ADD CONSTRAINT style_analysis_nodes_operation_check CHECK
 (operation=CASE phase WHEN 'registration' THEN 'pair.register' WHEN 'body_fit' THEN 'pair.fit.body'
  WHEN 'geometry' THEN 'geometry.body' WHEN 'geometry_registration' THEN 'pair.register'
  WHEN 'color_fit' THEN 'pair.fit.color' WHEN 'aggregate' THEN 'style.aggregate' ELSE operation END
  AND (phase<>'features' OR operation IN ('mask.person','pose.detect')));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_style_analysis_build()
RETURNS TABLE(tenant_id uuid,run_id uuid,style_id uuid,phase text,pair_count int,lease_token uuid,lease_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 RETURN QUERY WITH candidate AS (
  SELECT b.tenant_id,b.run_id FROM photo.style_analysis_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
  WHERE b.phase NOT IN ('complete','failed') AND (b.lease_until IS NULL OR b.lease_until<clock_timestamp())
   AND r.status IN ('queued','running') AND NOT r.cancel_requested AND (NOT r.graph_sealed OR
    (SELECT count(*) FROM photo.tasks t JOIN photo.style_analysis_nodes n ON n.tenant_id=t.tenant_id AND n.id=t.id
      WHERE t.tenant_id=b.tenant_id AND t.run_id=b.run_id AND t.state='succeeded' AND n.phase=
       CASE b.phase WHEN 'features' THEN 'registration' WHEN 'body_fit' THEN 'features' WHEN 'geometry' THEN 'body_fit'
        WHEN 'geometry_registration' THEN 'geometry' WHEN 'color_fit' THEN CASE WHEN b.graph_version=2 AND (r.config_snapshot->'analysis_options'->>'geometry_enabled')::boolean THEN 'geometry_registration' ELSE 'registration' END
        WHEN 'aggregate' THEN 'color_fit' ELSE '__none__' END)=
     CASE b.phase WHEN 'features' THEN b.pair_count WHEN 'body_fit' THEN b.pair_count*4 WHEN 'geometry' THEN b.pair_count
      WHEN 'geometry_registration' THEN b.pair_count WHEN 'color_fit' THEN b.pair_count WHEN 'aggregate' THEN b.pair_count ELSE -1 END)
  ORDER BY b.updated_at,b.run_id FOR UPDATE OF b SKIP LOCKED LIMIT 1
 ), opened AS (
  UPDATE photo.runs r SET graph_sealed=false,revision=revision+1 FROM candidate c
  WHERE r.tenant_id=c.tenant_id AND r.id=c.run_id AND r.graph_sealed RETURNING r.id
 ), claimed AS (
  UPDATE photo.style_analysis_builds b SET lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp()
  FROM candidate c WHERE b.tenant_id=c.tenant_id AND b.run_id=c.run_id AND EXISTS
   (SELECT FROM photo.runs r WHERE r.tenant_id=b.tenant_id AND r.id=b.run_id AND NOT r.graph_sealed) RETURNING b.*
 ) SELECT x.tenant_id,x.run_id,x.style_id,x.phase,x.pair_count,x.lease_token,x.lease_until FROM claimed x;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.advance_style_analysis_build(p_run uuid,p_token uuid,p_from text,p_to text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n int;expected int;
BEGIN
 IF p_to IS DISTINCT FROM (CASE p_from WHEN 'registration' THEN
   CASE WHEN (SELECT b.graph_version=2 AND (r.config_snapshot->'analysis_options'->>'geometry_enabled')::boolean FROM photo.runs r JOIN photo.style_analysis_builds b ON b.tenant_id=r.tenant_id AND b.run_id=r.id WHERE r.id=p_run) THEN 'features' ELSE 'color_fit' END
  WHEN 'features' THEN 'body_fit' WHEN 'body_fit' THEN 'geometry' WHEN 'geometry' THEN 'geometry_registration'
  WHEN 'geometry_registration' THEN 'color_fit' WHEN 'color_fit' THEN 'aggregate' WHEN 'aggregate' THEN 'complete' END) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.style_analysis_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
  WHERE b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>=clock_timestamp() AND b.phase=p_from
   AND NOT r.graph_sealed AND NOT r.cancel_requested AND r.status IN ('queued','running')) THEN RETURN false;END IF;
 expected:=CASE p_from WHEN 'features' THEN 4*(SELECT pair_count FROM photo.style_analysis_builds WHERE run_id=p_run)
  WHEN 'aggregate' THEN 1 ELSE (SELECT pair_count FROM photo.style_analysis_builds WHERE run_id=p_run) END;
 IF (SELECT count(*) FROM photo.tasks t JOIN photo.style_analysis_nodes sn ON sn.tenant_id=t.tenant_id AND sn.id=t.id
     WHERE t.run_id=p_run AND sn.phase=p_from AND t.state='blocked')<>expected THEN RETURN false;END IF;
 UPDATE photo.tasks t SET state='ready',next_attempt_at=clock_timestamp() FROM photo.style_analysis_nodes sn
  WHERE t.run_id=p_run AND t.tenant_id=sn.tenant_id AND t.id=sn.id AND sn.phase=p_from AND t.state='blocked';
 UPDATE photo.style_analysis_builds SET phase=p_to,lease_token=NULL,lease_until=NULL,last_error=NULL,updated_at=clock_timestamp()
  WHERE run_id=p_run AND lease_token=p_token AND lease_until>=clock_timestamp() AND phase=p_from;
 GET DIAGNOSTICS n=ROW_COUNT;
 IF n=1 THEN UPDATE photo.runs SET graph_sealed=true,status='running',revision=revision+1 WHERE id=p_run;END IF;
 RETURN n=1;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.register_style_analysis_node(p_tenant uuid,p_run uuid,p_token uuid,p_node jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.style_analysis_builds%ROWTYPE;old photo.style_analysis_nodes%ROWTYPE;entry jsonb;expected text;
BEGIN
 IF p_node IS NULL OR jsonb_typeof(p_node)<>'object' OR octet_length(p_node::text)>1048576
  OR (p_node-'id'-'phase'-'ordinal'-'pair_id'-'node_key'-'operation'-'inputs'-'prerequisites'-'params_sha256'-'params')<>'{}'
  OR p_node->>'params_sha256' !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_node->'inputs')<>'array'
  OR jsonb_typeof(p_node->'prerequisites')<>'array' OR jsonb_typeof(p_node->'params')<>'object' THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.style_analysis_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() OR b.phase<>p_node->>'phase' THEN RETURN false;END IF;
 expected:=CASE b.phase WHEN 'registration' THEN 'pair.register' WHEN 'body_fit' THEN 'pair.fit.body' WHEN 'geometry' THEN 'geometry.body'
  WHEN 'geometry_registration' THEN 'pair.register' WHEN 'color_fit' THEN 'pair.fit.color' WHEN 'aggregate' THEN 'style.aggregate' END;
 IF (b.phase='features' AND p_node->>'operation' NOT IN ('mask.person','pose.detect')) OR (b.phase<>'features' AND p_node->>'operation'<>expected)
  OR (p_node->>'ordinal')::int NOT BETWEEN 0 AND (CASE WHEN b.phase='features' THEN b.pair_count*4-1 ELSE b.pair_count-1 END)
  OR (b.phase='aggregate' AND (p_node->>'ordinal')::int<>0) THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_node->'inputs') LOOP
  IF (entry-'artifact_id'-'sha256')<>'{}' OR entry->>'sha256' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 IF b.phase<>'registration' AND EXISTS(SELECT FROM jsonb_array_elements_text(p_node->'prerequisites') x
  LEFT JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=x::uuid AND t.state='succeeded' WHERE t.id IS NULL) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.style_analysis_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_node->>'id')::uuid;
 IF FOUND THEN RETURN to_jsonb(old)-'tenant_id'-'run_id'=p_node;END IF;
 IF EXISTS(SELECT FROM photo.style_analysis_nodes WHERE id=(p_node->>'id')::uuid) THEN RETURN false;END IF;
 INSERT INTO photo.style_analysis_nodes(id,tenant_id,run_id,phase,ordinal,pair_id,node_key,operation,inputs,prerequisites,params_sha256,params)
 VALUES((p_node->>'id')::uuid,p_tenant,p_run,p_node->>'phase',(p_node->>'ordinal')::int,nullif(p_node->>'pair_id','')::uuid,p_node->>'node_key',p_node->>'operation',p_node->'inputs',ARRAY(SELECT jsonb_array_elements_text(p_node->'prerequisites')::uuid),p_node->>'params_sha256',p_node->'params');
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.style_analysis_pair_inputs(p_tenant uuid,p_run uuid,p_token uuid)
RETURNS TABLE(ordinal bigint,pair_id uuid,session_id text,payload jsonb,snapshot_digest text,config_snapshot jsonb)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT i.ordinal,i.item_id,i.payload->>'session_id',i.payload,s.digest,r.config_snapshot FROM photo.style_analysis_builds b
 JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
 JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id AND i.snapshot_id=s.id
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>=clock_timestamp()
  AND NOT r.graph_sealed AND b.phase NOT IN ('complete','failed') ORDER BY i.ordinal
$$;
-- +goose StatementEnd

DROP FUNCTION photo.style_analysis_stage_outputs(uuid,uuid,uuid,text,text);
-- +goose StatementBegin
CREATE FUNCTION photo.style_analysis_stage_outputs(p_tenant uuid,p_run uuid,p_token uuid,p_phase text,p_operation text,p_role text)
RETURNS TABLE(ordinal int,pair_id uuid,session_id text,task_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT n.ordinal,n.pair_id,i.payload->>'session_id',t.id,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type FROM photo.style_analysis_builds b
 JOIN photo.style_analysis_nodes n ON n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=p_phase AND n.operation=p_operation
 JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 JOIN photo.snapshot_items i ON i.tenant_id=r.tenant_id AND i.snapshot_id=r.input_snapshot_id AND i.item_id=n.pair_id
 JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL
 JOIN photo.task_results tr ON tr.tenant_id=t.tenant_id AND tr.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.produced_attempt=tr.attempt AND a.produced_fence=tr.fencing_token
  AND a.output_role=p_role AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>=clock_timestamp()
  AND p_phase IN ('registration','features','body_fit','geometry','geometry_registration','color_fit')
  AND tr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)) ORDER BY n.ordinal
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.style_analysis_stage_outputs(uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.style_analysis_stage_outputs(uuid,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.style_analysis_stage_outputs(uuid,uuid,uuid,text,text,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
