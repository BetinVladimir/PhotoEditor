-- +goose Up
-- Assemble Params reserve/complete revalidates and retains the full current assembly closure.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v30_delivery_assembly_params(p_expansion uuid,p_token uuid,p_body text,p_bucket text,p_tenant uuid,p_run uuid,p_owner uuid)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;d photo.backend_documents%ROWTYPE;tid uuid;rid uuid;owner uuid;sha text;size bigint;capacity numeric;used numeric;
 aid uuid:=gen_random_uuid();fence uuid:=gen_random_uuid();p_purpose constant text:='task_params';
BEGIN
 IF p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RAISE EXCEPTION 'INVALID_DELIVERY_BUCKET' USING ERRCODE='22023';END IF;
 scope:=photo.v30_delivery_assembly_params_scope(p_expansion,p_token,p_body);
 IF scope IS NULL OR scope->>'tenant' IS DISTINCT FROM p_tenant::text OR scope->>'run' IS DISTINCT FROM p_run::text OR scope->>'owner' IS DISTINCT FROM p_owner::text THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 tid:=(scope->>'tenant')::uuid;rid:=(scope->>'run')::uuid;owner:=(scope->>'owner')::uuid;sha:=scope->>'sha';size:=(scope->>'bytes')::bigint;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=tid AND b.run_id=rid AND b.owner_id=owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.sha256<>sha OR d.bytes<>size OR d.bucket<>p_bucket THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=tid AND a.id=d.artifact_id AND a.state='ready'
   AND a.sha256=sha AND a.bytes=size AND a.bucket=p_bucket AND a.object_version=d.object_version
   AND a.object_key='tenants/'||tid||'/runs/'||rid||'/documents/'||d.artifact_id AND a.media_type='application/json'
   AND a.output_role=p_purpose AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=p_expansion AND expansion_token=p_token AND expansion_lease_until>clock_timestamp()) THEN RAISE EXCEPTION 'DELIVERY_PARAMS_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
  RETURN QUERY SELECT 'resumed',d.artifact_id,fence,false;RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=tid;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=tid;
 IF capacity IS NULL OR used IS NULL OR used+size>capacity OR used+size>9007199254740991 THEN RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket)
 VALUES(tid,rid,owner,p_purpose,aid,fence,sha,size,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+size WHERE tenant_id=tid;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=p_expansion AND expansion_token=p_token AND expansion_lease_until>clock_timestamp()) THEN RAISE EXCEPTION 'DELIVERY_PARAMS_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
ALTER FUNCTION photo.reserve_v30_delivery_assembly_params(uuid,uuid,text,text,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v30_delivery_assembly_params(uuid,uuid,text,text,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v30_delivery_assembly_params(uuid,uuid,text,text,uuid,uuid,uuid) TO photo_scheduler;

CREATE FUNCTION photo.complete_v30_delivery_assembly_params(p_expansion uuid,p_token uuid,p_body text,p_bucket text,p_artifact uuid,p_document_token uuid,p_version text,p_tenant uuid,p_run uuid,p_owner uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;d photo.backend_documents%ROWTYPE;tid uuid;rid uuid;owner uuid;p_purpose constant text:='task_params';
BEGIN
 IF p_artifact IS NULL OR p_document_token IS NULL OR p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024 THEN RETURN false;END IF;
 scope:=photo.v30_delivery_assembly_params_scope(p_expansion,p_token,p_body);IF scope IS NULL OR scope->>'tenant' IS DISTINCT FROM p_tenant::text OR scope->>'run' IS DISTINCT FROM p_run::text OR scope->>'owner' IS DISTINCT FROM p_owner::text THEN RETURN false;END IF;
 tid:=(scope->>'tenant')::uuid;rid:=(scope->>'run')::uuid;owner:=(scope->>'owner')::uuid;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=tid AND b.run_id=rid AND b.owner_id=owner AND b.purpose=p_purpose FOR UPDATE;
 IF NOT FOUND OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_document_token OR d.bucket IS DISTINCT FROM p_bucket
 OR d.sha256 IS DISTINCT FROM scope->>'sha' OR d.bytes IS DISTINCT FROM (scope->>'bytes')::bigint THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=tid AND a.id=d.artifact_id
 AND a.state='ready' AND a.sha256=d.sha256 AND a.object_version=p_version AND a.verified_at IS NOT NULL
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,tid,d.bucket,'tenants/'||tid||'/runs/'||rid||'/documents/'||d.artifact_id,p_version,d.sha256,d.bytes,'application/json','ready',clock_timestamp(),p_purpose);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,storage_bytes=storage_bytes+d.bytes WHERE tenant_id=tid AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=d.id;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=p_expansion AND expansion_token=p_token AND expansion_lease_until>clock_timestamp()) THEN RAISE EXCEPTION 'DELIVERY_PARAMS_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v30_delivery_assembly_params(uuid,uuid,text,text,uuid,uuid,text,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v30_delivery_assembly_params(uuid,uuid,text,text,uuid,uuid,text,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v30_delivery_assembly_params(uuid,uuid,text,text,uuid,uuid,text,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
