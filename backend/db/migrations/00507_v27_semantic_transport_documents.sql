-- +goose Up
-- Fenced publication for staged semantic assembly; keep the graph sealed.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_transport_purpose(p text) RETURNS boolean
LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p='v2_input_manifest' OR CASE WHEN p ~ '^v2_input_chunk_([0-9]|[1-9][0-9]{1,3})$'
 THEN substring(p from 16)::integer BETWEEN 0 AND 1953 ELSE false END,false);
$$;
ALTER FUNCTION photo.v27_semantic_transport_purpose(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_transport_purpose(text) FROM PUBLIC;
CREATE FUNCTION photo.v27_semantic_transport_scope(p_tenant uuid,p_run uuid,p_task uuid,p_token uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.graph_expansions e ON e.tenant_id=t.tenant_id AND e.run_id=t.run_id
 JOIN photo.backend_documents d ON d.tenant_id=e.tenant_id AND d.run_id=e.run_id AND d.owner_id=e.id
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=p_task AND t.operation='v2.semantic.assemble'
 AND t.node_key='v2.semantic.assemble:0' AND t.operation_version='1' AND t.resource_class='cpu'
 AND t.state='blocked' AND t.attempt=0 AND NOT t.cancel_requested
 AND d.purpose='task_params' AND d.state='ready' AND d.artifact_id=t.params_artifact_id
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=d.sha256 AND a.object_version=d.object_version
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND photo.v27_semantic_assembly_lease_live(p_tenant,p_run,e.id,p_token))
 AND EXISTS(SELECT 1 FROM photo.v27_semantic_assembly_inputs(p_tenant,p_run));
$$;
ALTER FUNCTION photo.v27_semantic_transport_scope(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_transport_scope(uuid,uuid,uuid,uuid) FROM PUBLIC;
-- Prevent the unfenced legacy document route from bypassing this scope.
ALTER FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) RENAME TO v2_input_document_scope_pre_semantic_lease;
CREATE FUNCTION photo.v2_input_document_scope(p_tenant uuid,p_run uuid,p_task uuid,p_purpose text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task
 AND operation='v2.semantic.assemble') AND photo.v2_input_document_scope_pre_semantic_lease(p_tenant,p_run,p_task,p_purpose);
$$;
ALTER FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) FROM PUBLIC;
CREATE FUNCTION photo.reserve_v27_semantic_transport_document(p_tenant uuid,p_run uuid,
 p_task uuid,p_lease uuid,p_purpose text,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; capacity numeric; used numeric;
 aid uuid:=gen_random_uuid(); fence uuid:=gen_random_uuid();
BEGIN
 IF NOT photo.v27_semantic_transport_purpose(p_purpose) OR p_sha IS NULL
  OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RAISE EXCEPTION 'INVALID_V27_TRANSPORT_DOCUMENT' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF NOT photo.v27_semantic_transport_scope(p_tenant,p_run,p_task,p_lease) THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false; RETURN;
 END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_task AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false; RETURN;
  END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=d.tenant_id
    AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256
    AND a.object_version=d.object_version AND a.verified_at IS NOT NULL
    AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
    RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false; RETURN;
   END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true; RETURN;
  END IF;
  IF d.state<>'pending' THEN
   RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false; RETURN;
  END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',d.artifact_id,fence,false; RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity OR
  used+p_bytes>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false; RETURN;
 END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,
  artifact_id,token,sha256,bytes,bucket)
 VALUES(p_tenant,p_run,p_task,p_purpose,aid,fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
ALTER FUNCTION photo.reserve_v27_semantic_transport_document(uuid,uuid,uuid,uuid,text,text,bigint,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v27_semantic_transport_document(uuid,uuid,uuid,uuid,text,text,bigint,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v27_semantic_transport_document(uuid,uuid,uuid,uuid,text,text,bigint,text)
 TO photo_scheduler;

CREATE FUNCTION photo.complete_v27_semantic_transport_document(p_tenant uuid,p_run uuid,
 p_task uuid,p_lease uuid,p_purpose text,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,
 p_bucket text,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; target text;
BEGIN
 IF NOT photo.v27_semantic_transport_purpose(p_purpose) OR p_token IS NULL OR
  p_version IS NULL OR p_version IN ('','null') OR length(p_version)>1024 THEN RETURN false; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF NOT photo.v27_semantic_transport_scope(p_tenant,p_run,p_task,p_lease) THEN RETURN false; END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_task AND b.purpose=p_purpose FOR UPDATE;
 IF d.id IS NULL OR d.artifact_id IS DISTINCT FROM p_artifact OR
  d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM p_sha OR
  d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket THEN RETURN false; END IF;
 IF d.state='ready' THEN
  RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a
   WHERE a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.state='ready'
    AND a.sha256=d.sha256 AND a.object_version=p_version AND a.verified_at IS NOT NULL
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));
 END IF;
 IF d.state<>'pending' THEN RETURN false; END IF;
 target:='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,
  bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,d.tenant_id,d.bucket,target,p_version,d.sha256,d.bytes,
  'application/json','ready',clock_timestamp(),d.purpose);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,
  storage_bytes=storage_bytes+d.bytes
  WHERE tenant_id=d.tenant_id AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514'; END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,
  completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v27_semantic_transport_document(uuid,uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v27_semantic_transport_document(uuid,uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v27_semantic_transport_document(uuid,uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)
 TO photo_scheduler;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
