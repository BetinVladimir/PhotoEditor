-- +goose Up
CREATE TABLE photo.defect_branches (
 expansion_id uuid PRIMARY KEY,tenant_id uuid NOT NULL,run_id uuid NOT NULL,review_id uuid NOT NULL,
 completion_token uuid NOT NULL,payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 task_id uuid,base_artifact_id uuid,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(tenant_id,review_id),
 FOREIGN KEY(tenant_id,expansion_id) REFERENCES photo.graph_expansions(tenant_id,id),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id),
 FOREIGN KEY(tenant_id,review_id) REFERENCES photo.reviews(tenant_id,id),
 FOREIGN KEY(tenant_id,run_id,task_id) REFERENCES photo.tasks(tenant_id,run_id,id),
 FOREIGN KEY(tenant_id,base_artifact_id) REFERENCES photo.artifacts(tenant_id,id)
);
ALTER TABLE photo.defect_branches ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.defect_branches FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.defect_branches TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT ON photo.defect_branches TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.materialize_defect_branch(p_expansion uuid,p_token uuid,p_payload jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.graph_expansions%ROWTYPE;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;v photo.reviews%ROWTYPE;
 old photo.defect_branches%ROWTYPE;t jsonb;profile jsonb;ref jsonb;aid uuid;task_ref uuid;base_ref uuid;
 total_bytes numeric:=0;n numeric;pixels numeric;output_budget numeric;params_bytes bigint;timeout_value integer;
BEGIN
 IF p_token IS NULL OR p_payload IS NULL OR jsonb_typeof(p_payload) IS DISTINCT FROM 'object' OR octet_length(p_payload::text)>1048576
 OR (p_payload-'route'-'decision_hash'-'base'-'selection'-'task')<>'{}' THEN RAISE EXCEPTION 'INVALID_BRANCH' USING ERRCODE='22023';END IF;
 SELECT * INTO hint FROM photo.graph_expansions WHERE id=p_expansion;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE id=hint.run_id AND tenant_id=hint.tenant_id FOR UPDATE;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion FOR UPDATE;
 SELECT * INTO old FROM photo.defect_branches WHERE expansion_id=e.id;
 IF FOUND THEN RETURN old.completion_token=p_token AND old.payload=p_payload;END IF;
 IF r.cancel_requested OR r.graph_sealed OR r.status NOT IN ('running','waiting_review') OR e.state<>'open'
 OR e.expansion_token IS DISTINCT FROM p_token OR e.expansion_lease_until IS NULL OR e.expansion_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 SELECT vv.* INTO v FROM photo.reviews vv WHERE vv.tenant_id=e.tenant_id AND vv.run_id=e.run_id
 AND e.kind='review.defects/'||vv.id::text AND vv.task_id=e.parent_task_id AND vv.kind='defects' AND vv.state='decided';
 IF NOT FOUND OR v.decision_hash IS DISTINCT FROM p_payload->>'decision_hash' OR v.decision_route IS DISTINCT FROM p_payload->>'route'
 OR NOT EXISTS(SELECT FROM photo.tasks parent WHERE parent.tenant_id=e.tenant_id AND parent.run_id=e.run_id AND parent.id=e.parent_task_id AND parent.operation='defects.propose' AND parent.state='succeeded' AND parent.canonical_result_id IS NOT NULL) THEN RETURN false;END IF;
 IF v.decision_route='repair' THEN
  IF e.expected_count<>1 OR p_payload ? 'base' OR jsonb_typeof(p_payload->'task') IS DISTINCT FROM 'object' OR jsonb_typeof(p_payload->'selection') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
  t:=p_payload->'task';profile:=t->'execution_profile';
  IF (t-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'timeout_seconds'-'params'-'inputs'-'input_digest'-'execution_profile')<>'{}'
  OR t->>'node_key' IS DISTINCT FROM 'defects.repair/review/'||v.id::text OR t->>'operation' IS DISTINCT FROM 'defects.repair'
  OR t->>'operation_version' IS DISTINCT FROM '1' OR t->>'resource_class' IS DISTINCT FROM 'cpu'
  OR jsonb_typeof(t->'inputs') IS DISTINCT FROM 'array' OR jsonb_array_length(t->'inputs') NOT BETWEEN 1 AND 128
  OR t->>'input_digest' IS NULL OR t->>'input_digest' !~ '^[0-9a-f]{64}$'
  OR profile->>'backend' IS DISTINCT FROM 'cpu' OR profile->>'precision' IS DISTINCT FROM 'float32' THEN RETURN false;END IF;
  task_ref:=(t->>'id')::uuid;aid:=(t->'params'->>'artifact_id')::uuid;
  SELECT op.timeout_seconds INTO timeout_value FROM photo.operation_registry op WHERE op.operation='defects.repair' AND op.version='1';
  IF task_ref IS NULL OR aid IS NULL OR (t->>'timeout_seconds')::integer IS DISTINCT FROM timeout_value THEN RETURN false;END IF;
  SELECT a.bytes INTO params_bytes FROM photo.artifacts a JOIN photo.backend_documents b ON b.tenant_id=a.tenant_id AND b.artifact_id=a.id
   WHERE a.tenant_id=e.tenant_id AND a.id=aid AND a.sha256=t->'params'->>'sha256' AND a.state='ready' AND a.bytes<=1048576
   AND b.run_id=e.run_id AND b.owner_id=e.id AND b.purpose='task_params' AND b.state='ready';
  IF NOT FOUND THEN RETURN false;END IF;
  IF NOT EXISTS(SELECT FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
   WHERE b.tenant_id=e.tenant_id AND b.run_id=e.run_id AND b.owner_id=v.id AND b.purpose='review_selection' AND b.state='ready'
   AND a.state='ready' AND a.id=(p_payload->'selection'->>'artifact_id')::uuid AND a.sha256=p_payload->'selection'->>'sha256')
  OR NOT(t->'inputs' @> jsonb_build_array(p_payload->'selection')) THEN RETURN false;END IF;
  IF (SELECT count(DISTINCT x->>'artifact_id') FROM jsonb_array_elements(t->'inputs') x)<>jsonb_array_length(t->'inputs') THEN RETURN false;END IF;
  FOR ref IN SELECT value FROM jsonb_array_elements(t->'inputs') LOOP
   IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-'artifact_id'-'sha256')<>'{}' OR (ref->>'artifact_id')::uuid=aid THEN RETURN false;END IF;
   SELECT a.bytes INTO params_bytes FROM photo.artifacts a WHERE a.tenant_id=e.tenant_id AND a.id=(ref->>'artifact_id')::uuid AND a.sha256=ref->>'sha256' AND a.state='ready';
   IF NOT FOUND THEN RETURN false;END IF;
   total_bytes:=total_bytes+params_bytes;
  END LOOP;
  SELECT a.bytes INTO params_bytes FROM photo.artifacts a WHERE a.tenant_id=e.tenant_id AND a.id=aid;
  IF jsonb_typeof(profile->'resource_estimate') IS DISTINCT FROM 'object' OR
   (profile->'resource_estimate'-'ram_bytes'-'vram_bytes'-'scratch_bytes'-'output_bytes'-'pixel_count')<>'{}'
   OR (SELECT count(*) FROM jsonb_object_keys(profile->'resource_estimate'))<>5 THEN RETURN false;END IF;
  FOR ref IN SELECT value FROM jsonb_each(profile->'resource_estimate') LOOP
   IF jsonb_typeof(ref) IS DISTINCT FROM 'number' THEN RETURN false;END IF;
   n:=(ref::text)::numeric;IF n<0 OR n>9007199254740991 OR n<>trunc(n) THEN RETURN false;END IF;
  END LOOP;
  pixels:=(profile->'resource_estimate'->>'pixel_count')::numeric;
  output_budget:=12*pixels+8192+2097152;
  IF pixels IS NULL OR pixels<1 OR pixels>200000000 OR pixels<>trunc(pixels)
   OR coalesce((profile->'resource_estimate'->>'ram_bytes')::numeric,0)<2147483648
   OR (profile->'resource_estimate'->>'vram_bytes')::numeric IS DISTINCT FROM 0::numeric
   OR coalesce((profile->'resource_estimate'->>'output_bytes')::numeric,0)<output_budget
   OR coalesce((profile->'resource_estimate'->>'scratch_bytes')::numeric,0)<output_budget+total_bytes+params_bytes THEN RETURN false;END IF;
  INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,timeout_seconds,terminal_reason)
   VALUES(task_ref,e.tenant_id,e.run_id,t->>'node_key','defects.repair','1','cpu','blocked',t->'inputs',aid,t->>'input_digest',profile,timeout_value,'awaiting_continuation');
  INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition) VALUES(e.tenant_id,e.run_id,e.parent_task_id,task_ref,'accepted');
 ELSIF v.decision_route IN ('base','skip') THEN
  IF e.expected_count<>0 OR p_payload ? 'task' OR p_payload ? 'selection' THEN RETURN false;END IF;
  IF v.decision_route='base' THEN
   IF p_payload->'base' IS DISTINCT FROM v.selection->'image' THEN RETURN false;END IF;
   SELECT a.id INTO base_ref FROM photo.artifacts a WHERE a.tenant_id=e.tenant_id AND a.id=(p_payload->'base'->>'artifact_id')::uuid AND a.sha256=p_payload->'base'->>'sha256' AND a.state='ready';
   IF NOT FOUND THEN RETURN false;END IF;
  ELSIF p_payload ? 'base' THEN RETURN false;END IF;
 ELSE RETURN false;
 END IF;
 INSERT INTO photo.defect_branches(expansion_id,tenant_id,run_id,review_id,completion_token,payload,task_id,base_artifact_id)
 VALUES(e.id,e.tenant_id,e.run_id,v.id,p_token,p_payload,task_ref,base_ref);
 UPDATE photo.graph_expansions SET state='materialized',expansion_token=NULL,expansion_lease_until=NULL WHERE id=e.id;
 -- graph_sealed stays false until the continuation compiler binds the rest of the pipeline.
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.materialize_defect_branch(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_defect_branch(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.materialize_defect_branch(uuid,uuid,jsonb) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
