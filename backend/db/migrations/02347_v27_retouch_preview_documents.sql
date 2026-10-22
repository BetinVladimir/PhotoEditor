-- +goose Up
-- +goose StatementBegin
DO $$ DECLARE expression text;BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint
 WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check';
 IF expression IS NULL THEN RAISE EXCEPTION 'backend_documents purpose constraint missing';END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ((%s) OR purpose=''retouch_adapter_context'')',expression);
END $$;
-- +goose StatementEnd

-- Saved independent preview Params/transport publication; no approval or dispatch.
-- Final admission must independently revalidate exact transport content and scope.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_semantic_retouch_preview_document(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,p_body text,p_params_sha text,p_purpose text,p_task_owner uuid,p_reserved_artifact uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p_run uuid; d photo.backend_documents%ROWTYPE; capacity numeric; used numeric; config jsonb;
 aid uuid:=p_reserved_artifact; fence uuid:=gen_random_uuid();
BEGIN
 IF aid IS NULL OR aid='00000000-0000-0000-0000-000000000000'::uuid OR p_task_owner IS NULL OR p_task_owner='00000000-0000-0000-0000-000000000000'::uuid OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL
  OR p_bytes NOT BETWEEN 2 AND 1048576
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RAISE EXCEPTION 'SEMANTIC_TRANSPORT_INVALID' USING ERRCODE='22023';
 END IF;
 IF p_preview IS NULL OR p_preview='00000000-0000-0000-0000-000000000000'::uuid
 OR p_task_owner IS DISTINCT FROM photo.semantic_uuid_v5(p_preview,'semantic.preview_retouch_region:'||p_target::text)
 OR p_purpose IS NULL OR NOT(p_purpose IN('task_params','v2_input_manifest','retouch_adapter_context') OR
 (p_purpose ~ '^v2_input_chunk_([0-9]|[1-9][0-9]{1,3})$' AND substring(p_purpose from 16)::integer BETWEEN 0 AND 1953))
 OR (p_purpose<>'retouch_adapter_context' AND p_reserved_artifact IS DISTINCT FROM photo.semantic_uuid_v5(p_task_owner,CASE WHEN p_purpose='task_params' THEN 'Params' ELSE p_purpose END)) THEN RAISE EXCEPTION 'SEMANTIC_TRANSPORT_INVALID' USING ERRCODE='22023';END IF;
 p_run:=photo.lock_semantic_retouch_preview_publication(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,p_body,p_params_sha);
 SELECT config_snapshot->'semantic_retouch_preview' INTO config FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF p_purpose='retouch_adapter_context' AND (
 config->'adapter_context_ref'->>'artifact_id' IS DISTINCT FROM p_reserved_artifact::text
 OR config->'adapter_context_ref'->>'sha256' IS DISTINCT FROM p_sha
 OR octet_length(config->>'adapter_context_body')::bigint IS DISTINCT FROM p_bytes) THEN
 RAISE EXCEPTION 'SEMANTIC_RETOUCH_CONTEXT_CHANGED' USING ERRCODE='22023';END IF;

 IF p_purpose='task_params' AND (p_sha IS DISTINCT FROM p_params_sha OR p_bytes IS DISTINCT FROM octet_length(p_body)::bigint) THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_PARAMS_CHANGED' USING ERRCODE='22023';END IF;

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
    AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
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
ALTER FUNCTION photo.reserve_semantic_retouch_preview_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_semantic_retouch_preview_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,text,bigint,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.reserve_semantic_retouch_preview_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,text,bigint,text) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_semantic_retouch_preview_document(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,p_body text,p_params_sha text,p_purpose text,p_task_owner uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,
 p_bucket text,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p_run uuid; d photo.backend_documents%ROWTYPE; target text; config jsonb;
BEGIN
 IF p_task_owner IS NULL OR p_task_owner='00000000-0000-0000-0000-000000000000'::uuid OR p_token IS NULL OR p_version IS NULL OR p_version IN ('','null')
  OR length(p_version)>1024 THEN RETURN false; END IF;
 IF p_preview IS NULL OR p_preview='00000000-0000-0000-0000-000000000000'::uuid
 OR p_task_owner IS DISTINCT FROM photo.semantic_uuid_v5(p_preview,'semantic.preview_retouch_region:'||p_target::text)
 OR p_purpose IS NULL OR NOT(p_purpose IN('task_params','v2_input_manifest','retouch_adapter_context') OR
 (p_purpose ~ '^v2_input_chunk_([0-9]|[1-9][0-9]{1,3})$' AND substring(p_purpose from 16)::integer BETWEEN 0 AND 1953))
 OR (p_purpose<>'retouch_adapter_context' AND p_artifact IS DISTINCT FROM photo.semantic_uuid_v5(p_task_owner,CASE WHEN p_purpose='task_params' THEN 'Params' ELSE p_purpose END)) THEN RETURN false;END IF;
 p_run:=photo.lock_semantic_retouch_preview_publication(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,p_body,p_params_sha);
 SELECT config_snapshot->'semantic_retouch_preview' INTO config FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF p_purpose='retouch_adapter_context' AND (
 config->'adapter_context_ref'->>'artifact_id' IS DISTINCT FROM p_artifact::text
 OR config->'adapter_context_ref'->>'sha256' IS DISTINCT FROM p_sha
 OR octet_length(config->>'adapter_context_body')::bigint IS DISTINCT FROM p_bytes) THEN
 RAISE EXCEPTION 'SEMANTIC_RETOUCH_CONTEXT_CHANGED' USING ERRCODE='22023';END IF;

 IF p_purpose='task_params' AND (p_sha IS DISTINCT FROM p_params_sha OR p_bytes IS DISTINCT FROM octet_length(p_body)::bigint) THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_PARAMS_CHANGED' USING ERRCODE='22023';END IF;

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
    AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
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
ALTER FUNCTION photo.complete_semantic_retouch_preview_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_semantic_retouch_preview_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.complete_semantic_retouch_preview_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint,text,text) TO photo_api;


-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
