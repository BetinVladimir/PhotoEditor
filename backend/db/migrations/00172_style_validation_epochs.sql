-- Fenced independent holdout registration, validation and package epochs.
-- Existing graph versions remain immutable; only new builds default to v3.
-- +goose Up
ALTER TABLE photo.snapshots DROP CONSTRAINT snapshots_domain_0;
ALTER TABLE photo.snapshots ADD CONSTRAINT snapshots_domain_0
 CHECK (kind IN ('input','training','selection','download','style_import','holdout'));
ALTER TABLE photo.style_analysis_builds DROP CONSTRAINT style_analysis_builds_graph_version_check;
ALTER TABLE photo.style_analysis_builds ADD CONSTRAINT style_analysis_builds_graph_version_check CHECK(graph_version IN(1,2,3));
ALTER TABLE photo.style_analysis_builds ALTER COLUMN graph_version SET DEFAULT 3;
ALTER TABLE photo.style_analysis_builds DROP CONSTRAINT style_analysis_builds_phase_check;
ALTER TABLE photo.style_analysis_builds ADD CONSTRAINT style_analysis_builds_phase_check CHECK(phase IN('registration','features','body_fit','geometry','geometry_registration','color_fit','face_detect','face_semantics','face_skin','effects_fit','aggregate','holdout_features','holdout_registration','validate','package','complete','failed'));
ALTER TABLE photo.style_analysis_nodes DROP CONSTRAINT style_analysis_nodes_phase_check;
ALTER TABLE photo.style_analysis_nodes DROP CONSTRAINT style_analysis_nodes_operation_check;
ALTER TABLE photo.style_analysis_nodes DROP CONSTRAINT style_analysis_nodes_inputs_check;
ALTER TABLE photo.style_analysis_nodes ADD CONSTRAINT style_analysis_nodes_phase_check CHECK(phase IN('registration','features','body_fit','geometry','geometry_registration','color_fit','face_detect','face_semantics','face_skin','effects_fit','aggregate','holdout_features','holdout_registration','validate','package'));
ALTER TABLE photo.style_analysis_nodes ADD CONSTRAINT style_analysis_nodes_operation_check CHECK
 (operation=CASE phase WHEN 'registration' THEN 'pair.register' WHEN 'body_fit' THEN 'pair.fit.body' WHEN 'geometry' THEN 'geometry.body'
  WHEN 'geometry_registration' THEN 'pair.register' WHEN 'color_fit' THEN 'pair.fit.color' WHEN 'holdout_features' THEN 'mask.person' WHEN 'face_detect' THEN 'face.detect' WHEN 'face_skin' THEN 'mask.face_skin' WHEN 'aggregate' THEN 'style.aggregate'
  WHEN 'holdout_registration' THEN 'pair.register' WHEN 'validate' THEN 'style.validate' WHEN 'package' THEN 'style.package' ELSE operation END
  AND (phase<>'features' OR operation IN('mask.person','pose.detect')) AND (phase<>'face_semantics' OR operation IN('face.landmarks','face.parse')) AND (phase<>'effects_fit' OR operation IN('pair.fit.beauty','pair.fit.defects')));
ALTER TABLE photo.style_analysis_nodes ADD CONSTRAINT style_analysis_nodes_inputs_check CHECK(jsonb_typeof(inputs)='array' AND jsonb_array_length(inputs) BETWEEN 1 AND 512);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_style_analysis_build()
RETURNS TABLE(tenant_id uuid,run_id uuid,style_id uuid,phase text,pair_count int,lease_token uuid,lease_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 RETURN QUERY WITH eligible AS (
  SELECT b.tenant_id,b.run_id,b.phase,
   CASE b.phase WHEN 'features' THEN 'registration' WHEN 'body_fit' THEN 'features' WHEN 'geometry' THEN 'body_fit'
    WHEN 'geometry_registration' THEN 'geometry' WHEN 'color_fit' THEN CASE WHEN b.graph_version>=2 AND coalesce((r.config_snapshot->'analysis_options'->>'geometry_enabled')::boolean,false) THEN 'geometry_registration' ELSE 'registration' END
    WHEN 'face_detect' THEN 'color_fit' WHEN 'face_semantics' THEN 'face_detect' WHEN 'face_skin' THEN 'face_semantics' WHEN 'effects_fit' THEN 'face_skin' WHEN 'aggregate' THEN CASE WHEN b.graph_version>=3 THEN 'effects_fit' ELSE 'color_fit' END WHEN 'holdout_features' THEN 'aggregate' WHEN 'holdout_registration' THEN 'holdout_features' WHEN 'validate' THEN 'holdout_registration' WHEN 'package' THEN 'validate' END predecessor,
   CASE b.phase WHEN 'features' THEN b.pair_count WHEN 'body_fit' THEN b.pair_count*4 WHEN 'geometry' THEN b.pair_count WHEN 'geometry_registration' THEN b.pair_count
    WHEN 'color_fit' THEN b.pair_count WHEN 'face_detect' THEN b.pair_count WHEN 'face_semantics' THEN b.pair_count*2 WHEN 'face_skin' THEN b.pair_count*4 WHEN 'effects_fit' THEN b.pair_count*2 WHEN 'aggregate' THEN CASE WHEN b.graph_version>=3 THEN b.pair_count*2 ELSE b.pair_count END WHEN 'holdout_features' THEN 1 WHEN 'holdout_registration' THEN coalesce((SELECT s.item_count::int FROM photo.snapshots s WHERE s.tenant_id=b.tenant_id AND s.id=(r.config_snapshot->>'holdout_snapshot_id')::uuid AND s.kind='holdout'),-1)
    WHEN 'validate' THEN coalesce((SELECT s.item_count::int FROM photo.snapshots s WHERE s.tenant_id=b.tenant_id AND s.id=(r.config_snapshot->>'holdout_snapshot_id')::uuid AND s.kind='holdout'),-1)
    WHEN 'package' THEN 1 ELSE 0 END expected
  FROM photo.style_analysis_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
  WHERE b.phase NOT IN('complete','failed') AND (b.lease_until IS NULL OR b.lease_until<clock_timestamp()) AND r.status IN('queued','running') AND NOT r.cancel_requested
   AND (b.graph_version>=3 OR b.phase NOT IN('face_detect','face_semantics','face_skin','effects_fit')) AND (b.graph_version<3 OR b.phase NOT IN('holdout_features','holdout_registration','validate','package') OR r.config_snapshot ? 'holdout_snapshot_id')
 ), candidate AS (
  SELECT e.tenant_id,e.run_id FROM eligible e JOIN photo.runs r ON r.tenant_id=e.tenant_id AND r.id=e.run_id
  WHERE NOT r.graph_sealed OR e.phase='registration' OR (e.expected>=0 AND (SELECT count(*) FROM photo.style_analysis_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL WHERE n.tenant_id=e.tenant_id AND n.run_id=e.run_id AND n.phase=e.predecessor)=e.expected)
  ORDER BY e.run_id FOR UPDATE OF r SKIP LOCKED LIMIT 1
 ), opened AS (
  UPDATE photo.runs r SET graph_sealed=false,revision=revision+1 FROM candidate c WHERE r.tenant_id=c.tenant_id AND r.id=c.run_id AND r.graph_sealed RETURNING r.id
 ), claimed AS (
  UPDATE photo.style_analysis_builds b SET lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp() FROM candidate c
  WHERE b.tenant_id=c.tenant_id AND b.run_id=c.run_id AND EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=b.tenant_id AND r.id=b.run_id AND NOT r.graph_sealed) RETURNING b.*
 ) SELECT x.tenant_id,x.run_id,x.style_id,x.phase,x.pair_count,x.lease_token,x.lease_until FROM claimed x;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.advance_style_analysis_build(p_run uuid,p_token uuid,p_from text,p_to text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n int;expected int;wanted text;
BEGIN
 SELECT CASE p_from WHEN 'registration' THEN CASE WHEN b.graph_version>=2 AND coalesce((r.config_snapshot->'analysis_options'->>'geometry_enabled')::boolean,false) THEN 'features' ELSE 'color_fit' END
  WHEN 'features' THEN 'body_fit' WHEN 'body_fit' THEN 'geometry' WHEN 'geometry' THEN 'geometry_registration' WHEN 'geometry_registration' THEN 'color_fit'
  WHEN 'color_fit' THEN CASE WHEN b.graph_version>=3 THEN 'face_detect' ELSE 'aggregate' END WHEN 'face_detect' THEN 'face_semantics' WHEN 'face_semantics' THEN 'face_skin' WHEN 'face_skin' THEN 'effects_fit' WHEN 'effects_fit' THEN 'aggregate' WHEN 'aggregate' THEN CASE WHEN b.graph_version>=3 AND r.config_snapshot ? 'holdout_snapshot_id' THEN 'holdout_features' ELSE 'complete' END
  WHEN 'holdout_features' THEN 'holdout_registration' WHEN 'holdout_registration' THEN 'validate' WHEN 'validate' THEN 'package' WHEN 'package' THEN 'complete' END,
  CASE p_from WHEN 'features' THEN b.pair_count*4 WHEN 'face_detect' THEN b.pair_count*2 WHEN 'face_semantics' THEN b.pair_count*4 WHEN 'face_skin' THEN b.pair_count*2 WHEN 'effects_fit' THEN b.pair_count*2 WHEN 'aggregate' THEN 1 WHEN 'holdout_features' THEN (SELECT s.item_count::int FROM photo.snapshots s WHERE s.tenant_id=b.tenant_id AND s.id=(r.config_snapshot->>'holdout_snapshot_id')::uuid AND s.kind='holdout') WHEN 'holdout_registration' THEN (SELECT s.item_count::int FROM photo.snapshots s WHERE s.tenant_id=b.tenant_id AND s.id=(r.config_snapshot->>'holdout_snapshot_id')::uuid AND s.kind='holdout') WHEN 'validate' THEN 1 WHEN 'package' THEN 1 ELSE b.pair_count END
 INTO wanted,expected FROM photo.style_analysis_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id WHERE b.run_id=p_run;
 IF p_to IS DISTINCT FROM wanted OR expected IS NULL THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.style_analysis_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id WHERE b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>=clock_timestamp() AND b.phase=p_from AND NOT r.graph_sealed AND NOT r.cancel_requested AND r.status IN('queued','running')) THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.style_analysis_nodes sn JOIN photo.tasks t ON t.tenant_id=sn.tenant_id AND t.id=sn.id WHERE sn.run_id=p_run AND sn.phase=p_from AND t.state='blocked')<>expected THEN RETURN false;END IF;
 UPDATE photo.tasks t SET state='ready',next_attempt_at=clock_timestamp() FROM photo.style_analysis_nodes sn WHERE t.run_id=p_run AND t.tenant_id=sn.tenant_id AND t.id=sn.id AND sn.phase=p_from AND t.state='blocked';
 UPDATE photo.style_analysis_builds SET phase=p_to,lease_token=NULL,lease_until=NULL,last_error=NULL,updated_at=clock_timestamp() WHERE run_id=p_run AND lease_token=p_token AND lease_until>=clock_timestamp() AND phase=p_from;
 GET DIAGNOSTICS n=ROW_COUNT;IF n=1 THEN UPDATE photo.runs SET graph_sealed=true,status='running',revision=revision+1 WHERE id=p_run;END IF;RETURN n=1;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.register_style_analysis_node(p_tenant uuid,p_run uuid,p_token uuid,p_node jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.style_analysis_builds%ROWTYPE;r photo.runs%ROWTYPE;old photo.style_analysis_nodes%ROWTYPE;entry jsonb;expected text;max_ordinal int;
BEGIN
 IF p_node IS NULL OR jsonb_typeof(p_node)<>'object' OR octet_length(p_node::text)>1048576 OR (p_node-'id'-'phase'-'ordinal'-'pair_id'-'node_key'-'operation'-'inputs'-'prerequisites'-'params_sha256'-'params')<>'{}' OR p_node->>'params_sha256' !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_node->'inputs')<>'array' OR jsonb_typeof(p_node->'prerequisites')<>'array' OR jsonb_typeof(p_node->'params')<>'object' THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.style_analysis_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF b.run_id IS NULL OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() OR b.phase<>p_node->>'phase' THEN RETURN false;END IF;
 expected:=CASE b.phase WHEN 'registration' THEN 'pair.register' WHEN 'body_fit' THEN 'pair.fit.body' WHEN 'geometry' THEN 'geometry.body' WHEN 'geometry_registration' THEN 'pair.register' WHEN 'color_fit' THEN 'pair.fit.color' WHEN 'face_detect' THEN 'face.detect' WHEN 'face_skin' THEN 'mask.face_skin' WHEN 'aggregate' THEN 'style.aggregate' WHEN 'holdout_features' THEN 'mask.person' WHEN 'holdout_registration' THEN 'pair.register' WHEN 'validate' THEN 'style.validate' WHEN 'package' THEN 'style.package' END;
 max_ordinal:=CASE WHEN b.phase IN('features','face_semantics') THEN b.pair_count*4-1 WHEN b.phase IN('face_detect','face_skin','effects_fit') THEN b.pair_count*2-1 WHEN b.phase IN('holdout_features','holdout_registration') THEN coalesce((SELECT s.item_count::int-1 FROM photo.snapshots s WHERE s.tenant_id=p_tenant AND s.id=(r.config_snapshot->>'holdout_snapshot_id')::uuid AND s.kind='holdout'),-1) WHEN b.phase IN('aggregate','validate','package') THEN 0 ELSE b.pair_count-1 END;
 IF (b.phase='features' AND p_node->>'operation' NOT IN('mask.person','pose.detect')) OR (b.phase='face_semantics' AND p_node->>'operation' NOT IN('face.landmarks','face.parse')) OR (b.phase='effects_fit' AND p_node->>'operation' NOT IN('pair.fit.beauty','pair.fit.defects')) OR (b.phase NOT IN('features','face_semantics','effects_fit') AND p_node->>'operation'<>expected) OR (p_node->>'ordinal')::int NOT BETWEEN 0 AND max_ordinal THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_node->'inputs') LOOP IF (entry-'artifact_id'-'sha256')<>'{}' OR entry->>'sha256' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;END LOOP;
 IF b.phase<>'registration' AND EXISTS(SELECT FROM jsonb_array_elements_text(p_node->'prerequisites') x LEFT JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=x::uuid AND t.state='succeeded' WHERE t.id IS NULL) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.style_analysis_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_node->>'id')::uuid;IF FOUND THEN RETURN to_jsonb(old)-'tenant_id'-'run_id'=p_node;END IF;
 IF EXISTS(SELECT FROM photo.style_analysis_nodes WHERE id=(p_node->>'id')::uuid) THEN RETURN false;END IF;
 INSERT INTO photo.style_analysis_nodes(id,tenant_id,run_id,phase,ordinal,pair_id,node_key,operation,inputs,prerequisites,params_sha256,params) VALUES((p_node->>'id')::uuid,p_tenant,p_run,p_node->>'phase',(p_node->>'ordinal')::int,nullif(p_node->>'pair_id','')::uuid,p_node->>'node_key',p_node->>'operation',p_node->'inputs',ARRAY(SELECT jsonb_array_elements_text(p_node->'prerequisites')::uuid),p_node->>'params_sha256',p_node->'params');RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.style_analysis_holdout_inputs(p_tenant uuid,p_run uuid,p_token uuid)
RETURNS TABLE(ordinal bigint,pair_id uuid,session_id text,payload jsonb,snapshot_digest text,config_snapshot jsonb)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT i.ordinal,i.item_id,i.payload->>'session_id',i.payload,s.digest,r.config_snapshot FROM photo.style_analysis_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=(r.config_snapshot->>'holdout_snapshot_id')::uuid AND s.kind='holdout' JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id AND i.snapshot_id=s.id
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>=clock_timestamp() AND NOT r.graph_sealed AND b.graph_version>=3 AND b.phase IN('holdout_features','holdout_registration','validate') AND s.item_count BETWEEN 1 AND 64 AND (s.expires_at IS NULL OR s.expires_at>clock_timestamp()) ORDER BY i.ordinal
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.style_analysis_holdout_inputs(uuid,uuid,uuid) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.style_analysis_holdout_inputs(uuid,uuid,uuid) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.style_analysis_holdout_inputs(uuid,uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.style_analysis_holdout_outputs(p_tenant uuid,p_run uuid,p_token uuid,p_operation text,p_role text)
RETURNS TABLE(ordinal int,pair_id uuid,session_id text,task_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT n.ordinal,n.pair_id,i.payload->>'session_id',t.id,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type FROM photo.style_analysis_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=(r.config_snapshot->>'holdout_snapshot_id')::uuid AND s.kind='holdout' JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id AND i.snapshot_id=s.id JOIN photo.style_analysis_nodes n ON n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=CASE WHEN p_operation='mask.person' THEN 'holdout_features' ELSE 'holdout_registration' END AND n.operation=p_operation AND n.pair_id=i.item_id JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL JOIN photo.task_results tr ON tr.tenant_id=t.tenant_id AND tr.id=t.canonical_result_id JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.produced_attempt=tr.attempt AND a.produced_fence=tr.fencing_token AND a.output_role=p_role AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>=clock_timestamp() AND b.phase='validate' AND tr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)) ORDER BY n.ordinal
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.style_analysis_holdout_outputs(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.style_analysis_holdout_outputs(uuid,uuid,uuid,text,text) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.style_analysis_holdout_outputs(uuid,uuid,uuid,text,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.style_analysis_single_output(p_tenant uuid,p_run uuid,p_token uuid,p_phase text,p_operation text,p_role text)
RETURNS TABLE(task_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT t.id,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type FROM photo.style_analysis_builds b JOIN photo.style_analysis_nodes n ON n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=p_phase AND n.operation=p_operation JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL JOIN photo.task_results tr ON tr.tenant_id=t.tenant_id AND tr.id=t.canonical_result_id JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.produced_attempt=tr.attempt AND a.produced_fence=tr.fencing_token AND a.output_role=p_role AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>=clock_timestamp() AND tr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.style_analysis_single_output(uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.style_analysis_single_output(uuid,uuid,uuid,text,text,text) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.style_analysis_single_output(uuid,uuid,uuid,text,text,text) TO photo_scheduler;

-- Candidate selection waits for the final package on v3 holdout runs.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_style_candidate()
RETURNS TABLE(candidate_id uuid,token uuid,tenant_id uuid,run_id uuid,style_id uuid,config_id uuid,config_bucket text,config_sha text,config_key text,config_version text,config_bytes bigint,config_media text,report_id uuid,report_bucket text,report_sha text,report_key text,report_version text,report_bytes bigint,report_media text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE picked record;c photo.style_candidates%ROWTYPE;
BEGIN
 SELECT q.* INTO picked FROM (
  SELECT r.tenant_id,r.id run_id,(r.config_snapshot->>'style_id')::uuid style_id,config.id config_id,report.id report_id
  FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.operation='style.aggregate' AND t.operation_version='1' AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL
  JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
  JOIN photo.artifacts config ON config.tenant_id=t.tenant_id AND config.created_by_task_id=t.id AND config.output_role='style_candidate' AND config.state='ready' AND config.object_version IS NOT NULL AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',config.id::text,'sha256',config.sha256))
  JOIN photo.artifacts report ON report.tenant_id=t.tenant_id AND report.created_by_task_id=t.id AND report.output_role='aggregate_report' AND report.state='ready' AND report.object_version IS NOT NULL AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',report.id::text,'sha256',report.sha256))
  WHERE r.kind='analyze' AND NOT (r.config_snapshot ? 'holdout_snapshot_id') AND NOT r.cancel_requested
  UNION ALL
  SELECT r.tenant_id,r.id,(r.config_snapshot->>'style_id')::uuid,package.id,report.id
  FROM photo.runs r
  JOIN photo.style_analysis_nodes pn ON pn.tenant_id=r.tenant_id AND pn.run_id=r.id AND pn.phase='package' AND pn.operation='style.package'
  JOIN photo.tasks pt ON pt.tenant_id=pn.tenant_id AND pt.id=pn.id AND pt.state='succeeded' AND pt.canonical_result_id IS NOT NULL
  JOIN photo.task_results pz ON pz.tenant_id=pt.tenant_id AND pz.id=pt.canonical_result_id
  JOIN photo.artifacts package ON package.tenant_id=pt.tenant_id AND package.created_by_task_id=pt.id AND package.output_role='style_package' AND package.state='ready' AND package.object_version IS NOT NULL AND pz.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',package.id::text,'sha256',package.sha256))
  JOIN photo.style_analysis_nodes vn ON vn.tenant_id=r.tenant_id AND vn.run_id=r.id AND vn.phase='validate' AND vn.operation='style.validate'
  JOIN photo.tasks vt ON vt.tenant_id=vn.tenant_id AND vt.id=vn.id AND vt.state='succeeded' AND vt.canonical_result_id IS NOT NULL
  JOIN photo.task_results vz ON vz.tenant_id=vt.tenant_id AND vz.id=vt.canonical_result_id
  JOIN photo.artifacts validated ON validated.tenant_id=vt.tenant_id AND validated.created_by_task_id=vt.id AND validated.output_role='validated_style' AND validated.state='ready' AND validated.object_version IS NOT NULL AND vz.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',validated.id::text,'sha256',validated.sha256))
  JOIN photo.artifacts report ON report.tenant_id=vt.tenant_id AND report.created_by_task_id=vt.id AND report.output_role='quality_report' AND report.state='ready' AND report.object_version IS NOT NULL AND vz.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',report.id::text,'sha256',report.sha256))
  WHERE r.kind='analyze' AND r.config_snapshot ? 'holdout_snapshot_id' AND pt.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',validated.id::text,'sha256',validated.sha256)) AND pt.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',report.id::text,'sha256',report.sha256)) AND NOT r.cancel_requested
 ) q LEFT JOIN photo.style_candidates existing ON existing.tenant_id=q.tenant_id AND existing.training_run_id=q.run_id WHERE existing.id IS NULL ORDER BY q.run_id LIMIT 1;
 IF FOUND THEN INSERT INTO photo.style_candidates(tenant_id,style_id,training_run_id,config_artifact_id,report_artifact_id) VALUES(picked.tenant_id,picked.style_id,picked.run_id,picked.config_id,picked.report_id) RETURNING * INTO c;END IF;
 IF c.id IS NULL THEN SELECT * INTO c FROM photo.style_candidates x WHERE x.state='materializing' AND x.materialize_lease_until<=clock_timestamp() ORDER BY x.created_at,x.id LIMIT 1 FOR UPDATE SKIP LOCKED;IF c.id IS NOT NULL THEN UPDATE photo.style_candidates SET materialize_token=gen_random_uuid(),materialize_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=c.id RETURNING * INTO c;END IF;END IF;
 IF c.id IS NULL THEN RETURN;END IF;
 RETURN QUERY SELECT c.id,c.materialize_token,c.tenant_id,c.training_run_id,c.style_id,ca.id,ca.bucket,ca.sha256,ca.object_key,ca.object_version,ca.bytes,ca.media_type,ra.id,ra.bucket,ra.sha256,ra.object_key,ra.object_version,ra.bytes,ra.media_type FROM photo.artifacts ca,photo.artifacts ra WHERE ca.tenant_id=c.tenant_id AND ca.id=c.config_artifact_id AND ca.state='ready' AND ca.object_version IS NOT NULL AND ca.object_version NOT IN('','null') AND ra.tenant_id=c.tenant_id AND ra.id=c.report_artifact_id AND ra.state='ready' AND ra.object_version IS NOT NULL AND ra.object_version NOT IN('','null');
END $$;
-- +goose StatementEnd

-- Validation may declare up to 64 pairs x manifests/pixels/alpha/alignment.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.reserve_dispatch_resources(p_task uuid,p_node text,p_ram bigint,p_vram bigint,p_scratch bigint,p_event uuid,p_correlation uuid,p_trace text)
RETURNS TABLE(outcome text,command jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n photo.resource_nodes%ROWTYPE; t photo.tasks%ROWTYPE; hint photo.tasks%ROWTYPE; r photo.runs%ROWTYPE;
 slot photo.resource_slots%ROWTYPE; op photo.operation_registry%ROWTYPE; params photo.artifacts%ROWTYPE;
 used_ram numeric; used_vram numeric; used_scratch numeric; used_gpu integer; quota_gpu integer; until_time timestamptz;
 envelope jsonb; model jsonb; backend text; model_lock text; input jsonb; ai photo.artifacts%ROWTYPE;
BEGIN
 IF p_task IS NULL OR p_event IS NULL OR p_correlation IS NULL OR p_node IS NULL OR p_trace IS NULL OR length(p_trace)>128
 OR p_ram IS NULL OR p_vram IS NULL OR p_scratch IS NULL OR p_ram<=0 OR p_vram<0 OR p_scratch<0 THEN
 RAISE EXCEPTION 'INVALID_RESERVATION' USING ERRCODE='22023'; END IF;
 SELECT * INTO hint FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND THEN RETURN QUERY SELECT 'missing',NULL::jsonb; RETURN; END IF;
 SELECT * INTO n FROM photo.resource_nodes WHERE node_id=p_node FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR NOT n.enabled OR n.reported_at IS NULL OR n.reported_at<clock_timestamp()-interval '30 seconds' OR n.doctor_report_sha256 IS NULL THEN
 RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE node_id=p_node AND resource_class=hint.resource_class AND task_id IS NULL ORDER BY slot_name LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=hint.tenant_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'TENANT_USAGE_MISSING'; END IF;
 SELECT * INTO r FROM photo.runs WHERE id=hint.run_id AND tenant_id=hint.tenant_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE id=p_task FOR UPDATE;
 IF t.state<>'ready' OR t.attempt>=6 OR t.tenant_id<>hint.tenant_id OR t.run_id<>hint.run_id OR t.resource_class<>slot.resource_class THEN
 RETURN QUERY SELECT 'stale',NULL::jsonb; RETURN; END IF;
 IF t.cancel_requested OR r.cancel_requested OR r.status NOT IN ('queued','running') THEN
 RETURN QUERY SELECT 'cancelled',NULL::jsonb; RETURN; END IF;
 IF NOT r.graph_sealed THEN RETURN QUERY SELECT 'unsealed',NULL::jsonb; RETURN; END IF;
 SELECT * INTO op FROM photo.operation_registry WHERE operation=t.operation AND version=t.operation_version;
 IF NOT FOUND OR op.resource_class<>t.resource_class THEN RAISE EXCEPTION 'OPERATION_MISMATCH' USING ERRCODE='22023'; END IF;
 backend:=t.execution_profile->>'backend';
 IF backend IS NULL OR NOT op.backends ? backend THEN RETURN QUERY SELECT 'unsupported_backend',NULL::jsonb; RETURN; END IF;
 model:=NULL;
 IF op.model_id IS NOT NULL THEN
  model_lock:=t.execution_profile->>'model_lock_id';
  IF model_lock IS NULL OR model_lock !~ '^[0-9a-f]{64}$' THEN RETURN QUERY SELECT 'unqualified_model',NULL::jsonb; RETURN; END IF;
  model:=jsonb_build_object('id',op.model_id,'lock_id',model_lock);
 END IF;
 IF NOT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id AND w.node_id=p_node AND w.enabled
 AND w.db_principal IS NOT NULL AND w.resource_classes ? t.resource_class AND w.backends ? backend AND (w.qualified_operations->t.operation) ? backend
 AND w.last_seen>clock_timestamp()-interval '30 seconds' AND (op.model_id IS NULL OR w.lock_id=model_lock)) THEN
 RETURN QUERY SELECT 'waiting_worker',NULL::jsonb; RETURN; END IF;
 SELECT coalesce(sum(ram_bytes),0),coalesce(sum(vram_bytes),0),coalesce(sum(scratch_bytes),0),coalesce(sum(gpu_units),0)
 INTO used_ram,used_vram,used_scratch,used_gpu FROM photo.resource_reservations WHERE node_id=p_node AND state='reserved';
 IF used_ram+p_ram>least(n.ram_budget_bytes,n.ram_available_bytes) OR used_vram+p_vram>least(n.vram_budget_bytes,n.vram_available_bytes)
 OR used_scratch+p_scratch>least(n.scratch_budget_bytes,n.scratch_available_bytes) OR (t.resource_class='gpu' AND used_gpu>=1) THEN
 RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 SELECT coalesce((quotas->>'max_active_gpu_tasks')::integer,1) INTO quota_gpu FROM photo.tenants WHERE id=t.tenant_id;
 IF t.resource_class='gpu' AND (SELECT active_gpu_tasks>=quota_gpu FROM photo.tenant_usage WHERE tenant_id=t.tenant_id) THEN
 RETURN QUERY SELECT 'waiting_quota',NULL::jsonb; RETURN; END IF;
 -- Artifact locks are acquired after task, in UUID order, including params.
 IF jsonb_typeof(t.inputs)<>'array' OR jsonb_array_length(t.inputs)>512 THEN RAISE EXCEPTION 'INVALID_INPUTS'; END IF;
 FOR input IN SELECT value FROM jsonb_array_elements(t.inputs) LOOP
  IF jsonb_typeof(input)<>'object' OR (input-'artifact_id'-'sha256')<>'{}' OR input->>'artifact_id' IS NULL OR input->>'sha256' IS NULL OR input->>'sha256' !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'INVALID_INPUTS' USING ERRCODE='22023'; END IF;
 END LOOP;
 IF EXISTS(SELECT FROM photo.resource_reservations WHERE tenant_id=t.tenant_id AND task_id=t.id AND state='reserved') THEN
 RETURN QUERY SELECT 'stale',NULL::jsonb; RETURN; END IF;
 FOR input IN SELECT value FROM jsonb_array_elements(t.inputs || jsonb_build_array(jsonb_build_object('artifact_id',t.params_artifact_id))) ORDER BY value->>'artifact_id' LOOP
  SELECT * INTO ai FROM photo.artifacts WHERE tenant_id=t.tenant_id AND id=(input->>'artifact_id')::uuid FOR SHARE;
  IF NOT FOUND OR ai.state<>'ready' OR (ai.expires_at IS NOT NULL AND ai.expires_at<=clock_timestamp()) OR
   (input ? 'sha256' AND ai.sha256 IS DISTINCT FROM input->>'sha256') THEN RETURN QUERY SELECT 'input_unavailable',NULL::jsonb; RETURN; END IF;
  IF ai.id=t.params_artifact_id THEN params:=ai; END IF;
 END LOOP;
 envelope:=jsonb_build_object('schema_version',1,'event_id',p_event,'tenant_id',t.tenant_id,'run_id',t.run_id,'task_id',t.id,
 'attempt',t.attempt+1,'operation',t.operation,'operation_version',t.operation_version,'resource_class',t.resource_class,'correlation_id',p_correlation,
 'inputs',t.inputs,'params',jsonb_build_object('artifact_id',params.id,'sha256',params.sha256),'model',model,'timeout_seconds',op.timeout_seconds,'traceparent',p_trace);
 IF octet_length(envelope::text)>65536 THEN RAISE EXCEPTION 'COMMAND_TOO_LARGE' USING ERRCODE='22023'; END IF;
 until_time:=clock_timestamp()+interval '60 seconds';
 INSERT INTO photo.resource_reservations(tenant_id,task_id,node_id,scratch_bytes,ram_bytes,vram_bytes,state,expires_at,fencing_token,gpu_units)
 VALUES(t.tenant_id,t.id,p_node,p_scratch,p_ram,p_vram,'reserved',until_time,t.fencing_token,CASE WHEN t.resource_class='gpu' THEN 1 ELSE 0 END)
 ON CONFLICT(tenant_id,task_id) DO UPDATE SET node_id=excluded.node_id,scratch_bytes=excluded.scratch_bytes,ram_bytes=excluded.ram_bytes,vram_bytes=excluded.vram_bytes,
 state='reserved',expires_at=excluded.expires_at,fencing_token=excluded.fencing_token,gpu_units=excluded.gpu_units;
 UPDATE photo.resource_slots SET task_id=t.id,tenant_id=t.tenant_id,fencing_token=t.fencing_token,lease_until=until_time WHERE id=slot.id;
 IF t.resource_class='gpu' THEN UPDATE photo.tenant_usage SET active_gpu_tasks=active_gpu_tasks+1 WHERE tenant_id=t.tenant_id; END IF;
 UPDATE photo.tasks SET state='queued',dispatch_command=envelope,timeout_seconds=op.timeout_seconds,terminal_reason=NULL WHERE id=t.id;
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(t.tenant_id,p_event,'photo.commands.v1',op.routing_key,envelope);
 RETURN QUERY SELECT 'queued',envelope;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
