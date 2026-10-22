-- +goose Up
ALTER TABLE photo.editor_effect_builds
 ADD COLUMN seal_adoption_digest text CHECK(seal_adoption_digest ~ '^[0-9a-f]{64}$'),
 ADD COLUMN seal_adoption_sql_digest text CHECK(seal_adoption_sql_digest ~ '^[0-9a-f]{64}$');
-- +goose StatementBegin
CREATE FUNCTION photo.editor_effect_seal_adoption_scope(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS SETOF photo.editor_effect_builds
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT b.* FROM photo.editor_effect_builds b
 JOIN photo.tenants tenant ON tenant.id=b.tenant_id AND tenant.status='active'
 JOIN photo.runs origin ON origin.tenant_id=b.tenant_id AND origin.id=b.run_id AND NOT origin.cancel_requested
 JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.seal_run_id AND r.kind='v2' AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id AND w.project_id=b.project_id AND w.workflow_type='provenance' AND w.renderer_version='v2.2'
 JOIN photo.tasks t ON t.tenant_id=b.tenant_id AND t.id=b.seal_task_id AND t.run_id=r.id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=p_result AND z.task_id=t.id
 WHERE b.tenant_id=p_tenant AND b.state='sealing' AND t.id=p_task AND t.operation='editor.effects_seal' AND t.operation_version='1' AND t.resource_class='cpu'
 AND t.execution_profile->>'backend'='cpu' AND t.state='succeeded' AND NOT t.cancel_requested
 AND t.attempt=p_attempt AND t.fencing_token=p_fence AND t.canonical_result_id=z.id AND z.attempt=p_attempt AND z.fencing_token=p_fence
 AND t.params_artifact_id=b.seal_params_artifact_id AND photo.v2_editor_authorized(p_tenant,b.project_id,b.actor_id,b.session_id)
 AND EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=b.seal_params_artifact_id AND a.sha256=b.seal_params_sha
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 AND EXISTS(SELECT FROM photo.tasks producer JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=b.producer_result_id AND result.task_id=producer.id
  WHERE producer.tenant_id=p_tenant AND producer.id=b.producer_task_id AND producer.run_id=b.run_id AND producer.state='succeeded' AND NOT producer.cancel_requested
   AND producer.canonical_result_id=result.id AND producer.attempt=b.producer_attempt AND producer.fencing_token=b.producer_fence
   AND result.attempt=b.producer_attempt AND result.fencing_token=b.producer_fence
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',b.processed_source_artifact_id,'sha256',b.processed_source_sha)))
 AND EXISTS(SELECT FROM photo.tasks quality JOIN photo.task_results result ON result.tenant_id=quality.tenant_id AND result.id=b.quality_result_id AND result.task_id=quality.id
  WHERE quality.tenant_id=p_tenant AND quality.id=b.quality_task_id AND quality.run_id=b.run_id AND quality.operation='quality.check'
   AND quality.state='succeeded' AND NOT quality.cancel_requested AND quality.canonical_result_id=result.id
   AND result.attempt=quality.attempt AND result.fencing_token=quality.fencing_token
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',b.quality_artifact_id,'sha256',b.quality_sha)))
 AND (SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.created_by_task_id=t.id AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence
  AND a.output_role='executed_base_receipt' AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)))=1
$$;
ALTER FUNCTION photo.editor_effect_seal_adoption_scope(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_seal_adoption_scope(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
CREATE FUNCTION photo.editor_effect_seal_adoption_context(p_tenant uuid,p_task uuid)
RETURNS TABLE(attempt integer,fence bigint,result_id uuid,operation text,operation_version text,root_artifact_id uuid,root_sha256 text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.attempt,t.fencing_token,t.canonical_result_id,t.operation,t.operation_version,a.id,a.sha256
 FROM photo.tasks t CROSS JOIN LATERAL photo.editor_effect_seal_adoption_scope(p_tenant,p_task,t.attempt,t.fencing_token,t.canonical_result_id) b
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND a.output_role='executed_base_receipt' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT FROM photo.task_results z WHERE z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
  AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)))
$$;
ALTER FUNCTION photo.editor_effect_seal_adoption_context(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_seal_adoption_context(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_effect_seal_adoption_context(uuid,uuid) TO photo_scheduler;
CREATE FUNCTION photo.editor_effect_seal_adoption_inputs(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(params_artifact_id uuid,params_sha text,execution_profile jsonb,build_run_id uuid)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT b.seal_params_artifact_id,b.seal_params_sha,t.execution_profile,b.run_id
 FROM photo.editor_effect_seal_adoption_scope(p_tenant,p_task,p_attempt,p_fence,p_result) b
 JOIN photo.tasks t ON t.tenant_id=b.tenant_id AND t.id=p_task
$$;
ALTER FUNCTION photo.editor_effect_seal_adoption_inputs(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_seal_adoption_inputs(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_effect_seal_adoption_inputs(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
CREATE FUNCTION photo.editor_effect_seal_adoption_artifact(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_artifact uuid)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text,role text,source text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,
  CASE WHEN u.id IS NOT NULL THEN u.role ELSE NULL END,CASE WHEN u.id IS NOT NULL THEN 'output' ELSE 'input' END
 FROM photo.tasks t CROSS JOIN LATERAL photo.editor_effect_seal_adoption_scope(p_tenant,p_task,p_attempt,p_fence,t.canonical_result_id) b
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=p_artifact
 LEFT JOIN photo.output_uploads u ON u.tenant_id=t.tenant_id AND u.task_id=t.id AND u.artifact_id=a.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.state='ready'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND ((u.id IS NOT NULL AND a.created_by_task_id=t.id AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence
  AND u.sha256=a.sha256 AND u.role=a.output_role AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)))
 OR (u.id IS NULL AND (t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
  OR a.id=b.seal_params_artifact_id AND a.sha256=b.seal_params_sha)))
$$;
ALTER FUNCTION photo.editor_effect_seal_adoption_artifact(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_seal_adoption_artifact(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_effect_seal_adoption_artifact(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
CREATE FUNCTION photo.editor_effect_seal_adoption_staged_outputs(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint)
RETURNS TABLE(artifact_id uuid,sha256 text,state text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT u.artifact_id,u.sha256,u.state FROM photo.tasks t
 CROSS JOIN LATERAL photo.editor_effect_seal_adoption_scope(p_tenant,p_task,p_attempt,p_fence,t.canonical_result_id) b
 JOIN photo.output_uploads u ON u.tenant_id=t.tenant_id AND u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence
 WHERE t.tenant_id=p_tenant AND t.id=p_task
$$;
ALTER FUNCTION photo.editor_effect_seal_adoption_staged_outputs(uuid,uuid,integer,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_seal_adoption_staged_outputs(uuid,uuid,integer,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_effect_seal_adoption_staged_outputs(uuid,uuid,integer,bigint) TO photo_scheduler;
CREATE FUNCTION photo.editor_effect_pending_seal_adoptions() RETURNS TABLE(tenant_id uuid,run_id uuid,task_id uuid)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT b.tenant_id,b.seal_run_id,b.seal_task_id FROM photo.editor_effect_builds b JOIN photo.tasks t ON t.tenant_id=b.tenant_id AND t.id=b.seal_task_id
 WHERE b.state='sealing' AND EXISTS(SELECT FROM photo.editor_effect_seal_adoption_scope(b.tenant_id,t.id,t.attempt,t.fencing_token,t.canonical_result_id))
 ORDER BY b.updated_at,b.run_id LIMIT 100
$$;
ALTER FUNCTION photo.editor_effect_pending_seal_adoptions() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_pending_seal_adoptions() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_effect_pending_seal_adoptions() TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_editor_effect_seal(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE; t photo.tasks%ROWTYPE; w photo.v2_workflows%ROWTYPE;
 q photo.quota_reservations%ROWTYPE; item record; evidence record; root photo.artifacts%ROWTYPE;
 image photo.artifacts%ROWTYPE; sql_digest text;
BEGIN
 IF p_digest !~ '^[0-9a-f]{64}$' OR p_digest IS NULL OR jsonb_typeof(p_inventory) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_inventory) NOT BETWEEN 1 AND 4096 THEN RAISE EXCEPTION 'EDITOR_SEAL_INVENTORY_INVALID' USING ERRCODE='22023';END IF;
 sql_digest:=encode(public.digest(convert_to(p_inventory::text,'UTF8'),'sha256'),'hex');
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND seal_task_id=p_task;
 IF b.run_id IS NULL THEN RETURN false;END IF;
 PERFORM 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=b.seal_run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=b.run_id FOR UPDATE;
 IF t.canonical_result_id IS DISTINCT FROM p_result OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence THEN RETURN false;END IF;
 IF b.seal_adoption_digest IS NOT NULL THEN
  IF b.seal_adoption_digest<>p_digest OR b.seal_adoption_sql_digest<>sql_digest THEN RAISE EXCEPTION 'EDITOR_SEAL_REPLAY_CONFLICT' USING ERRCODE='23514';END IF;
  RETURN b.state IN ('sealed','composing','complete');
 END IF;
 IF NOT EXISTS(SELECT FROM photo.editor_effect_seal_adoption_scope(p_tenant,p_task,p_attempt,p_fence,p_result)) THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=b.seal_run_id FOR UPDATE;
 FOR item IN SELECT value,ordinality-1 AS ordinal FROM jsonb_array_elements(p_inventory) WITH ORDINALITY LOOP
  IF jsonb_typeof(item.value)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(item.value))<>4
   OR NOT(item.value ?& ARRAY['artifact_id','sha256','purpose','ordinal'])
   OR coalesce(item.value->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR coalesce(item.value->>'sha256','') !~ '^[0-9a-f]{64}$' OR coalesce(item.value->>'purpose','') !~ '^[0-9a-f]{64}$'
   OR item.value->>'ordinal' IS DISTINCT FROM item.ordinal::text THEN RAISE EXCEPTION 'EDITOR_SEAL_INVENTORY_INVALID' USING ERRCODE='22023';END IF;
  SELECT * INTO evidence FROM photo.editor_effect_seal_adoption_artifact(p_tenant,p_task,p_attempt,p_fence,(item.value->>'artifact_id')::uuid);
  IF NOT FOUND OR evidence.sha256 IS DISTINCT FROM item.value->>'sha256' THEN RAISE EXCEPTION 'EDITOR_SEAL_ARTIFACT_UNQUALIFIED' USING ERRCODE='23514';END IF;
  INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal)
   VALUES(p_tenant,b.project_id,w.id,(item.value->>'artifact_id')::uuid,item.value->>'sha256',item.value->>'purpose',item.ordinal);
 END LOOP;
 IF EXISTS(SELECT FROM photo.output_uploads u WHERE u.tenant_id=p_tenant AND u.task_id=p_task AND u.attempt=p_attempt AND u.fence=p_fence
  AND (u.state<>'ready' OR NOT EXISTS(SELECT FROM jsonb_array_elements(p_inventory) e WHERE e->>'artifact_id'=u.artifact_id::text AND e->>'sha256'=u.sha256))) THEN
  RAISE EXCEPTION 'EDITOR_SEAL_OUTPUT_INCOMPLETE' USING ERRCODE='23514';END IF;
 SELECT a.* INTO STRICT root FROM photo.artifacts a JOIN photo.task_results z ON z.tenant_id=a.tenant_id AND z.id=p_result
  WHERE a.tenant_id=p_tenant AND a.created_by_task_id=p_task AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence
  AND a.output_role='executed_base_receipt' AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256));
 SELECT a.* INTO STRICT image FROM photo.artifacts a JOIN photo.task_results z ON z.tenant_id=a.tenant_id AND z.id=p_result
  WHERE a.tenant_id=p_tenant AND a.created_by_task_id=p_task AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence
  AND a.output_role='image_result' AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256));
 IF p_inventory->0->>'artifact_id' IS DISTINCT FROM root.id::text OR p_inventory->0->>'sha256' IS DISTINCT FROM root.sha256 THEN
  RAISE EXCEPTION 'EDITOR_SEAL_ROOT_INVALID' USING ERRCODE='23514';END IF;
 UPDATE photo.editor_effect_builds SET state='sealed',result_artifact_id=image.id,result_sha=image.sha256,
  seal_receipt_artifact_id=root.id,seal_receipt_sha=root.sha256,seal_adoption_digest=p_digest,seal_adoption_sql_digest=sql_digest,updated_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND run_id=b.run_id;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=b.seal_run_id AND state='reserved' FOR UPDATE;
 IF q.id IS NOT NULL THEN
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
  IF NOT FOUND THEN RAISE EXCEPTION 'EDITOR_SEAL_RESERVATION_INCONSISTENT' USING ERRCODE='23514';END IF;
  UPDATE photo.quota_reservations SET bytes=0,state='consumed' WHERE tenant_id=p_tenant AND id=q.id;
 END IF;
 UPDATE photo.runs SET status='completed',revision=revision+1 WHERE tenant_id=p_tenant AND id=b.seal_run_id AND revision<9007199254740991;
 IF NOT FOUND THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.adopt_editor_effect_seal(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_editor_effect_seal(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_editor_effect_seal(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- Forward-only: verified immutable receipts and retained closure cannot be rolled back.
