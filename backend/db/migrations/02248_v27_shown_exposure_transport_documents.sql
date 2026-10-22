-- +goose Up
-- API-scoped shown-copy Params/transport publication before child admission; no task shell or dispatch.
-- Final admission must independently revalidate exact transport content and scope.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_semantic_shown_transport_document(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_target uuid,p_target_revision bigint,p_plan_sha text,p_candidate_sha text,p_params_body text,p_params_sha text,p_child uuid,p_purpose text,p_task_owner uuid,p_reserved_artifact uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p_run uuid; d photo.backend_documents%ROWTYPE; capacity numeric; used numeric;
 aid uuid:=p_reserved_artifact; fence uuid:=gen_random_uuid();
BEGIN
 IF aid IS NULL OR aid='00000000-0000-0000-0000-000000000000'::uuid OR p_task_owner IS NULL OR p_task_owner='00000000-0000-0000-0000-000000000000'::uuid OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL
  OR p_bytes NOT BETWEEN 2 AND 1048576
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RAISE EXCEPTION 'SEMANTIC_TRANSPORT_INVALID' USING ERRCODE='22023';
 END IF;
 IF p_child IS NULL OR p_child='00000000-0000-0000-0000-000000000000'::uuid
 OR p_task_owner IS DISTINCT FROM photo.semantic_uuid_v5(p_child,'semantic.adopt_shown_exposure:'||p_target::text)
 OR p_purpose IS NULL OR NOT(p_purpose IN('task_params','v2_input_manifest') OR
 (p_purpose ~ '^v2_input_chunk_([0-9]|[1-9][0-9]{1,3})$' AND substring(p_purpose from 16)::integer BETWEEN 0 AND 1953))
 OR p_reserved_artifact IS DISTINCT FROM photo.semantic_uuid_v5(p_task_owner,CASE WHEN p_purpose='task_params' THEN 'Params' ELSE p_purpose END) THEN RAISE EXCEPTION 'SEMANTIC_TRANSPORT_INVALID' USING ERRCODE='22023';END IF;
 IF p_purpose='task_params' AND (p_sha IS DISTINCT FROM p_params_sha OR p_bytes IS DISTINCT FROM octet_length(p_params_body)::bigint) THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_PARAMS_BYTES_INVALID' USING ERRCODE='22023';END IF;
 p_run:=photo.lock_semantic_shown_publication(p_tenant,p_intent,p_actor,p_session,p_child,p_target,p_revision,p_target_revision,p_plan_sha,p_candidate_sha,p_params_body,p_params_sha,p_task_owner);
 IF p_child=p_run THEN RAISE EXCEPTION 'SEMANTIC_TRANSPORT_CHILD_INVALID' USING ERRCODE='22023';END IF;

 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_task_owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id IS DISTINCT FROM p_reserved_artifact OR d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false; RETURN; END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
    AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256
    AND a.object_version=d.object_version AND a.bytes=d.bytes AND a.bucket=d.bucket
    AND a.media_type='application/json' AND a.output_role=p_purpose AND a.verified_at IS NOT NULL
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
    RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false; RETURN; END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true; RETURN;
  END IF;
  IF d.state<>'pending' THEN
   RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false; RETURN; END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',d.artifact_id,fence,false; RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity
  OR used+p_bytes>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false; RETURN; END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,
  artifact_id,token,sha256,bytes,bucket)
 VALUES(p_tenant,p_run,p_task_owner,p_purpose,aid,fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_semantic_shown_transport_document(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,text,text,uuid,text,uuid,uuid,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_semantic_shown_transport_document(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,text,text,uuid,text,uuid,uuid,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_semantic_shown_transport_document(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,text,text,uuid,text,uuid,uuid,text,bigint,text) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_semantic_shown_transport_document(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_target uuid,p_target_revision bigint,p_plan_sha text,p_candidate_sha text,p_params_body text,p_params_sha text,p_child uuid,p_purpose text,p_task_owner uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,
 p_bucket text,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p_run uuid; d photo.backend_documents%ROWTYPE; target text;
BEGIN
 IF p_task_owner IS NULL OR p_task_owner='00000000-0000-0000-0000-000000000000'::uuid OR p_token IS NULL OR p_version IS NULL OR p_version IN ('','null')
  OR length(p_version)>1024 THEN RETURN false; END IF;
 IF p_child IS NULL OR p_child='00000000-0000-0000-0000-000000000000'::uuid
 OR p_task_owner IS DISTINCT FROM photo.semantic_uuid_v5(p_child,'semantic.adopt_shown_exposure:'||p_target::text)
 OR p_purpose IS NULL OR NOT(p_purpose IN('task_params','v2_input_manifest') OR
 (p_purpose ~ '^v2_input_chunk_([0-9]|[1-9][0-9]{1,3})$' AND substring(p_purpose from 16)::integer BETWEEN 0 AND 1953))
 OR p_artifact IS DISTINCT FROM photo.semantic_uuid_v5(p_task_owner,CASE WHEN p_purpose='task_params' THEN 'Params' ELSE p_purpose END) THEN RETURN false;END IF;
 IF p_purpose='task_params' AND (p_sha IS DISTINCT FROM p_params_sha OR p_bytes IS DISTINCT FROM octet_length(p_params_body)::bigint) THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_PARAMS_BYTES_INVALID' USING ERRCODE='22023';END IF;
 p_run:=photo.lock_semantic_shown_publication(p_tenant,p_intent,p_actor,p_session,p_child,p_target,p_revision,p_target_revision,p_plan_sha,p_candidate_sha,p_params_body,p_params_sha,p_task_owner);
 IF p_child=p_run THEN RAISE EXCEPTION 'SEMANTIC_TRANSPORT_CHILD_INVALID' USING ERRCODE='22023';END IF;

 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_task_owner AND b.purpose=p_purpose FOR UPDATE;
 IF d.id IS NULL OR d.artifact_id IS DISTINCT FROM p_artifact OR
  d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM p_sha OR
  d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket
  THEN RETURN false; END IF;
 IF d.state='ready' THEN
  RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a
   WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.state='ready'
    AND a.sha256=d.sha256 AND a.object_version=p_version AND a.bytes=d.bytes AND a.bucket=d.bucket
    AND a.media_type='application/json' AND a.output_role=p_purpose AND a.verified_at IS NOT NULL
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())); END IF;
 IF d.state<>'pending' THEN RETURN false; END IF;
 target:='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,
  bytes,media_type,state,verified_at,output_role)
 VALUES(p_artifact,p_tenant,p_bucket,target,p_version,p_sha,p_bytes,
  'application/json','ready',clock_timestamp(),p_purpose);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,
  storage_bytes=storage_bytes+d.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514'; END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,
  completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_semantic_shown_transport_document(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,text,text,uuid,text,uuid,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_semantic_shown_transport_document(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,text,text,uuid,text,uuid,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_semantic_shown_transport_document(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,text,text,uuid,text,uuid,uuid,uuid,text,bigint,text,text) TO photo_api;

