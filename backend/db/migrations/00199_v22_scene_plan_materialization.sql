-- +goose Up
-- Preserve the admission-pinned model lock after the pending recovery packet
-- is replaced by the final ticket. The workflow FK cascades on tenant purge.
CREATE TABLE photo.v22_analysis_locks (
 workflow_id uuid PRIMARY KEY,
 tenant_id uuid NOT NULL,
 model_lock_sha text NOT NULL CHECK(model_lock_sha ~ '^[0-9a-f]{64}$'),
 UNIQUE(tenant_id,workflow_id),
 FOREIGN KEY(tenant_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,id)
  ON DELETE CASCADE
);
ALTER TABLE photo.v22_analysis_locks ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v22_analysis_locks FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v22_analysis_locks TO photo_fn_owner
 USING(true) WITH CHECK(true);
REVOKE ALL ON photo.v22_analysis_locks FROM PUBLIC;
GRANT SELECT,INSERT ON photo.v22_analysis_locks TO photo_fn_owner;

ALTER TABLE photo.session_analyses ADD COLUMN plan_body_digest text
 CHECK(plan_body_digest ~ '^[0-9a-f]{64}$');

-- +goose StatementBegin
CREATE FUNCTION photo.pin_v22_analysis_model_lock() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE workflow uuid; saved text;
BEGIN
 IF NEW.route<>'V2.Admit' OR NEW.principal_type<>'user'
  OR NEW.response_status IS NOT NULL OR NEW.resource_id IS NULL
  OR jsonb_typeof(NEW.response_body) IS DISTINCT FROM 'object'
  OR coalesce(NEW.response_body->>'model_lock_sha','') !~ '^[0-9a-f]{64}$' THEN
  RETURN NEW;
 END IF;
 SELECT w.id INTO workflow FROM photo.v2_workflows w
  JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
  WHERE w.tenant_id=NEW.tenant_id AND w.run_id=NEW.resource_id
   AND w.workflow_type='session' AND r.kind='v2' AND r.status='draft'
   AND r.created_by=NEW.principal_id;
 IF workflow IS NULL THEN
  RAISE EXCEPTION 'V22_MODEL_LOCK_WORKFLOW_INVALID' USING ERRCODE='23514';
 END IF;
 INSERT INTO photo.v22_analysis_locks(workflow_id,tenant_id,model_lock_sha)
 VALUES(workflow,NEW.tenant_id,NEW.response_body->>'model_lock_sha')
 ON CONFLICT(workflow_id) DO NOTHING;
 SELECT l.model_lock_sha INTO saved FROM photo.v22_analysis_locks l
  WHERE l.workflow_id=workflow AND l.tenant_id=NEW.tenant_id;
 IF saved IS DISTINCT FROM NEW.response_body->>'model_lock_sha' THEN
  RAISE EXCEPTION 'V22_MODEL_LOCK_CONFLICT' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.pin_v22_analysis_model_lock() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pin_v22_analysis_model_lock() FROM PUBLIC;
CREATE TRIGGER v22_analysis_model_lock_pin
 AFTER UPDATE OF response_body ON photo.idempotency_records
 FOR EACH ROW EXECUTE FUNCTION photo.pin_v22_analysis_model_lock();

-- The caller reads the versioned object and verifies its exact canonical
-- bytes against p_plan_sha before invoking this function. SQL rechecks the
-- canonical adopted result and the complete frozen snapshot partition.
-- +goose StatementBegin
CREATE FUNCTION photo.v22_pending_scene_plan_materializations()
RETURNS TABLE(tenant_id uuid,run_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.tenant_id,r.id FROM photo.runs r
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.v22_analysis_locks l ON l.tenant_id=w.tenant_id
  AND l.workflow_id=w.id
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
  AND t.operation='session.plan' AND t.node_key='session.plan:0'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id
  AND z.id=t.canonical_result_id AND z.task_id=t.id
 WHERE r.kind='v2' AND r.status='waiting_review' AND NOT r.cancel_requested
  AND w.workflow_type='session' AND w.renderer_version='v2.2'
  AND t.state='succeeded' AND z.review_required
  AND NOT EXISTS(SELECT 1 FROM photo.session_analyses a
   WHERE a.tenant_id=w.tenant_id AND a.workflow_id=w.id)
 ORDER BY r.created_at,r.id LIMIT 32;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_pending_scene_plan_materializations()
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_pending_scene_plan_materializations() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_pending_scene_plan_materializations()
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v22_scene_plan_materialization_source(p_tenant uuid,p_run uuid)
RETURNS TABLE(snapshot_id uuid,snapshot_digest text,item_count bigint,
 policy_sha text,model_lock_sha text,artifact_id uuid,sha256 text,bytes bigint,bucket text,
 object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT s.id,s.digest,s.item_count,w.policy_sha,l.model_lock_sha,a.id,a.sha256,a.bytes,
  a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id
  AND w.run_id=r.id
 JOIN photo.v22_analysis_locks l ON l.tenant_id=w.tenant_id
  AND l.workflow_id=w.id
 JOIN photo.snapshots s ON s.tenant_id=r.tenant_id
  AND s.id=r.input_snapshot_id
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
  AND t.operation='session.plan' AND t.node_key='session.plan:0'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id
  AND z.id=t.canonical_result_id AND z.task_id=t.id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id
  AND a.id=(z.outputs->0->>'artifact_id')::uuid
  AND a.sha256=z.outputs->0->>'sha256'
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
  AND r.status='waiting_review' AND r.graph_sealed AND NOT r.cancel_requested
  AND w.workflow_type='session' AND w.renderer_version='v2.2'
  AND w.input_snapshot_id=s.id AND s.kind='v2'
  AND s.item_count BETWEEN 1 AND 5000
  AND t.state='succeeded' AND NOT t.cancel_requested
  AND z.review_required AND z.attempt=t.attempt
  AND z.fencing_token=t.fencing_token
  AND jsonb_array_length(z.outputs)=1
  AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id
   AND i.consumer='v2-adopt'
   AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
  AND a.state='ready' AND a.output_role='document'
  AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 8388608
  AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt
  AND a.produced_fence=t.fencing_token AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_scene_plan_materialization_source(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_scene_plan_materialization_source(uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_scene_plan_materialization_source(uuid,uuid)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v22_scene_input_snapshot_source(p_tenant uuid,p_run uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,
 object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id
  AND w.run_id=r.id
 JOIN photo.backend_documents d ON d.tenant_id=r.tenant_id
  AND d.run_id=r.id AND d.owner_id=r.id
  AND d.purpose='v2_analysis_input_snapshot' AND d.state='ready'
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  AND a.sha256=d.sha256 AND a.object_version=d.object_version
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
  AND r.status='waiting_review' AND w.workflow_type='session'
  AND w.renderer_version='v2.2' AND a.state='ready'
  AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 8388608
  AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
  AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_scene_input_snapshot_source(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_scene_input_snapshot_source(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_scene_input_snapshot_source(uuid,uuid)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v22_scene_snapshot_members(p_tenant uuid,p_run uuid)
RETURNS TABLE(ordinal bigint,asset_version_id uuid,artifact_id uuid,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT i.ordinal,i.asset_version_id,i.artifact_id,i.sha256
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id
  AND w.run_id=r.id
 JOIN photo.snapshot_items i ON i.tenant_id=r.tenant_id
  AND i.snapshot_id=r.input_snapshot_id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
  AND r.status='waiting_review' AND w.workflow_type='session'
  AND w.renderer_version='v2.2'
 ORDER BY i.ordinal;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_scene_snapshot_members(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_scene_snapshot_members(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_scene_snapshot_members(uuid,uuid)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v22_scene_correction_sources(p_tenant uuid,p_run uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,
 object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT DISTINCT a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id
  AND w.run_id=r.id
 JOIN photo.tasks task ON task.tenant_id=r.tenant_id
  AND task.run_id=r.id AND task.operation='session.plan'
  AND task.node_key='session.plan:0' AND task.state='succeeded'
 JOIN photo.task_results result ON result.tenant_id=task.tenant_id
  AND result.id=task.canonical_result_id AND result.task_id=task.id
 JOIN photo.artifacts a ON a.tenant_id=task.tenant_id
  AND a.created_by_task_id=task.id AND a.produced_attempt=result.attempt
  AND a.produced_fence=result.fencing_token
 JOIN photo.v2_artifact_links link ON link.tenant_id=a.tenant_id
  AND link.workflow_id=w.id AND link.artifact_id=a.id AND link.sha256=a.sha256
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
  AND r.status='waiting_review' AND w.workflow_type='session'
  AND w.renderer_version='v2.2' AND a.state='ready'
  AND a.output_role='correction_plan' AND a.media_type='application/json'
  AND a.bytes BETWEEN 2 AND 8388608 AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 ORDER BY a.id;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_scene_correction_sources(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_scene_correction_sources(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_scene_correction_sources(uuid,uuid)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.materialize_v22_scene_plan(
 p_tenant uuid,p_run uuid,p_plan_artifact uuid,p_plan_sha text,
 p_plan jsonb,p_model_lock_sha text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE;
 t photo.tasks%ROWTYPE; z photo.task_results%ROWTYPE;
 s photo.snapshots%ROWTYPE; existing photo.session_analyses%ROWTYPE;
 group_doc jsonb; group_id uuid; group_ordinal integer:=0;
 member jsonb; member_ordinal integer; member_version uuid;
 reference_version uuid; analysis_id uuid; seen_groups uuid[]:='{}';
 seen_ordinals integer[]:='{}'; correction_id uuid; correction_sha text;
 body_digest text;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_plan_artifact IS NULL
  OR coalesce(p_plan_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_model_lock_sha,'') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(p_plan) IS DISTINCT FROM 'object'
  OR pg_column_size(p_plan)>8388608
  OR (p_plan-'schema_version'-'snapshot_id'-'snapshot_digest'-'policy_sha'-'groups')<>'{}'::jsonb
  OR NOT (p_plan ?& ARRAY['schema_version','snapshot_id','snapshot_digest','policy_sha','groups'])
  OR p_plan->>'schema_version' IS DISTINCT FROM '1'
  OR jsonb_typeof(p_plan->'groups') IS DISTINCT FROM 'array'
  OR jsonb_array_length(p_plan->'groups') NOT BETWEEN 1 AND 5000 THEN
  RAISE EXCEPTION 'V22_SCENE_PLAN_INVALID' USING ERRCODE='22023';
 END IF;
 body_digest:=encode(sha256(convert_to(p_plan::text,'UTF8')),'hex');
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 SELECT * INTO s FROM photo.snapshots WHERE tenant_id=p_tenant
  AND id=r.input_snapshot_id;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'waiting_review'
  OR r.cancel_requested OR NOT r.graph_sealed OR w.id IS NULL
  OR w.workflow_type<>'session' OR w.renderer_version<>'v2.2'
  OR w.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
  OR NOT EXISTS(SELECT 1 FROM photo.v22_analysis_locks l
   WHERE l.tenant_id=p_tenant AND l.workflow_id=w.id
    AND l.model_lock_sha=p_model_lock_sha)
  OR s.id IS NULL OR s.kind<>'v2' OR s.item_count NOT BETWEEN 1 AND 5000
  OR s.digest IS DISTINCT FROM p_plan->>'snapshot_digest'
  OR s.id::text IS DISTINCT FROM p_plan->>'snapshot_id'
  OR w.policy_sha IS DISTINCT FROM p_plan->>'policy_sha'
  OR NOT EXISTS(SELECT 1 FROM photo.tenants tenant
   WHERE tenant.id=p_tenant AND tenant.status='active') THEN
  RAISE EXCEPTION 'V22_SCENE_PLAN_STALE' USING ERRCODE='40001';
 END IF;
 SELECT * INTO t FROM photo.tasks task WHERE task.tenant_id=p_tenant
  AND task.run_id=p_run AND task.operation='session.plan'
  AND task.node_key='session.plan:0' FOR UPDATE;
 IF t.id IS NULL OR t.state<>'succeeded' OR t.canonical_result_id IS NULL
  OR t.cancel_requested OR t.operation_version<>'1'
  OR NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
   AND i.consumer='v2-adopt'
   AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)) THEN
  RAISE EXCEPTION 'V22_SCENE_PLAN_NOT_ADOPTED' USING ERRCODE='23514';
 END IF;
 SELECT * INTO z FROM photo.task_results result WHERE result.tenant_id=p_tenant
  AND result.id=t.canonical_result_id AND result.task_id=t.id;
 IF z.id IS NULL OR NOT z.review_required
  OR z.attempt IS DISTINCT FROM t.attempt
  OR z.fencing_token IS DISTINCT FROM t.fencing_token
  OR z.outputs IS DISTINCT FROM jsonb_build_array(jsonb_build_object(
   'artifact_id',p_plan_artifact,'sha256',p_plan_sha))
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=p_plan_artifact AND a.sha256=p_plan_sha AND a.state='ready'
   AND a.output_role='document' AND a.media_type='application/json'
   AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt
   AND a.produced_fence=t.fencing_token AND a.verified_at IS NOT NULL
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'V22_SCENE_PLAN_ROOT_INVALID' USING ERRCODE='23514';
 END IF;
 SELECT * INTO existing FROM photo.session_analyses x WHERE x.tenant_id=p_tenant
  AND x.workflow_id=w.id;
 IF existing.id IS NOT NULL THEN
  IF existing.plan_artifact_id=p_plan_artifact AND existing.snapshot_id=s.id
   AND existing.policy_sha=w.policy_sha
   AND existing.model_lock_sha=p_model_lock_sha
   AND existing.plan_body_digest=body_digest THEN RETURN existing.id; END IF;
  RAISE EXCEPTION 'V22_SCENE_PLAN_CONFLICT' USING ERRCODE='23514';
 END IF;
 IF (SELECT count(*) FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant
  AND i.snapshot_id=s.id AND i.ordinal BETWEEN 0 AND s.item_count-1
  AND i.asset_version_id IS NOT NULL)<>s.item_count THEN
  RAISE EXCEPTION 'V22_SCENE_SNAPSHOT_INCOMPLETE' USING ERRCODE='23514';
 END IF;
 FOR group_doc IN SELECT value FROM jsonb_array_elements(p_plan->'groups') LOOP
  IF jsonb_typeof(group_doc) IS DISTINCT FROM 'object'
   OR (group_doc-'id'-'ordinals'-'reference_ordinal'-'confidence'-'corrections')<>'{}'::jsonb
   OR NOT (group_doc ?& ARRAY['id','ordinals','reference_ordinal','confidence','corrections'])
   OR coalesce(group_doc->>'id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR jsonb_typeof(group_doc->'ordinals') IS DISTINCT FROM 'array'
   OR jsonb_array_length(group_doc->'ordinals') NOT BETWEEN 1 AND 5000
   OR coalesce(group_doc->>'reference_ordinal','') !~ '^(0|[1-9][0-9]{0,3})$'
   OR jsonb_typeof(group_doc->'confidence') IS DISTINCT FROM 'number'
   OR (group_doc->>'confidence')::numeric NOT BETWEEN 0 AND 1
   OR jsonb_typeof(group_doc->'corrections') IS DISTINCT FROM 'object'
   OR ((group_doc->'corrections')-'artifact_id'-'sha256')<>'{}'::jsonb
   OR coalesce(group_doc->'corrections'->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR coalesce(group_doc->'corrections'->>'sha256','') !~ '^[0-9a-f]{64}$' THEN
   RAISE EXCEPTION 'V22_SCENE_GROUP_INVALID' USING ERRCODE='22023';
  END IF;
  group_id:=(group_doc->>'id')::uuid;
  correction_id:=(group_doc->'corrections'->>'artifact_id')::uuid;
  correction_sha:=group_doc->'corrections'->>'sha256';
  IF group_id=ANY(seen_groups) OR group_id='00000000-0000-0000-0000-000000000000'::uuid
   OR NOT EXISTS(SELECT 1 FROM photo.artifacts a
    JOIN photo.v2_artifact_links link ON link.tenant_id=a.tenant_id
     AND link.artifact_id=a.id AND link.sha256=a.sha256
     AND link.workflow_id=w.id
    WHERE a.tenant_id=p_tenant AND a.id=correction_id AND a.sha256=correction_sha
     AND a.state='ready' AND a.output_role='correction_plan'
     AND a.media_type='application/json' AND a.created_by_task_id=t.id
     AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
     AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
     AND a.object_version NOT IN('','null')
     AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
   RAISE EXCEPTION 'V22_SCENE_CORRECTION_INVALID' USING ERRCODE='23514';
  END IF;
  seen_groups:=array_append(seen_groups,group_id);
  reference_version:=NULL;
  FOR member IN SELECT value FROM jsonb_array_elements(group_doc->'ordinals') LOOP
   IF jsonb_typeof(member) IS DISTINCT FROM 'number'
    OR member::text !~ '^(0|[1-9][0-9]{0,3})$' THEN
    RAISE EXCEPTION 'V22_SCENE_ORDINAL_INVALID' USING ERRCODE='22023';
   END IF;
   member_ordinal:=member::text::integer;
   IF member_ordinal>=s.item_count OR member_ordinal=ANY(seen_ordinals) THEN
    RAISE EXCEPTION 'V22_SCENE_ORDINAL_DUPLICATE' USING ERRCODE='23514';
   END IF;
   SELECT i.asset_version_id INTO member_version FROM photo.snapshot_items i
    WHERE i.tenant_id=p_tenant AND i.snapshot_id=s.id AND i.ordinal=member_ordinal;
   IF member_version IS NULL THEN
    RAISE EXCEPTION 'V22_SCENE_MEMBER_MISSING' USING ERRCODE='23514';
   END IF;
   seen_ordinals:=array_append(seen_ordinals,member_ordinal);
   IF member_ordinal=(group_doc->>'reference_ordinal')::integer THEN
    reference_version:=member_version;
   END IF;
  END LOOP;
  IF reference_version IS NULL THEN
   RAISE EXCEPTION 'V22_SCENE_REFERENCE_INVALID' USING ERRCODE='23514';
  END IF;
  group_ordinal:=group_ordinal+1;
 END LOOP;
 IF cardinality(seen_ordinals)<>s.item_count THEN
  RAISE EXCEPTION 'V22_SCENE_PARTITION_INCOMPLETE' USING ERRCODE='23514';
 END IF;
 analysis_id:=gen_random_uuid();
 INSERT INTO photo.session_analyses(id,tenant_id,project_id,session_id,
  workflow_id,snapshot_id,plan_artifact_id,policy_sha,model_lock_sha,plan_body_digest)
 VALUES(analysis_id,p_tenant,w.project_id,(r.config_snapshot->>'session_id')::uuid,
  w.id,s.id,p_plan_artifact,w.policy_sha,p_model_lock_sha,body_digest);
 group_ordinal:=0;
 FOR group_doc IN SELECT value FROM jsonb_array_elements(p_plan->'groups') LOOP
  group_id:=(group_doc->>'id')::uuid;
  SELECT i.asset_version_id INTO reference_version FROM photo.snapshot_items i
   WHERE i.tenant_id=p_tenant AND i.snapshot_id=s.id
    AND i.ordinal=(group_doc->>'reference_ordinal')::integer;
  INSERT INTO photo.scene_groups(id,tenant_id,project_id,analysis_id,
   ordinal,reference_version_id,confidence)
  VALUES(group_id,p_tenant,w.project_id,analysis_id,group_ordinal,
   reference_version,(group_doc->>'confidence')::double precision);
  FOR member IN SELECT value FROM jsonb_array_elements(group_doc->'ordinals') LOOP
   member_ordinal:=member::text::integer;
   SELECT i.asset_version_id INTO member_version FROM photo.snapshot_items i
    WHERE i.tenant_id=p_tenant AND i.snapshot_id=s.id AND i.ordinal=member_ordinal;
   INSERT INTO photo.scene_members(tenant_id,project_id,group_id,
    asset_version_id,snapshot_ordinal)
   VALUES(p_tenant,w.project_id,group_id,member_version,member_ordinal);
  END LOOP;
  group_ordinal:=group_ordinal+1;
 END LOOP;
 RETURN analysis_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.materialize_v22_scene_plan(uuid,uuid,uuid,text,jsonb,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_v22_scene_plan(uuid,uuid,uuid,text,jsonb,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.materialize_v22_scene_plan(uuid,uuid,uuid,text,jsonb,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
