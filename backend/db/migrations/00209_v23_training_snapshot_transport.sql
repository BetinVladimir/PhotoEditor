-- V23 deterministic document IDs allow an ArtifactSnapshot digest before
-- draft creation; both documents remain quota-accounted and immutable.
-- +goose Up
ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check
 CHECK (purpose IN ('task_params','review_selection','v2_input_manifest',
  'v2_session_plan_params','v2_analysis_input_snapshot','v2_analysis_context',
  'v2_analysis_consents','v23_training_manifest','v23_training_snapshot') OR
  CASE WHEN purpose ~ '^v2_input_chunk_([0-9]|[1-9][0-9]{1,3})$'
   THEN substring(purpose from 16)::integer BETWEEN 0 AND 1953 ELSE false END OR
  CASE WHEN purpose ~ '^v2_analysis_describe_params_([0-9]|[1-3][0-9])$'
   THEN true ELSE false END);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v23_training_document_scope(p_tenant uuid,p_run uuid,
 p_owner uuid,p_purpose text,p_sha text) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; q photo.quota_reservations%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_owner IS NULL OR
  p_purpose NOT IN ('v23_training_manifest','v23_training_snapshot') OR
  p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant
  AND holder_type='run' AND holder_id=p_run;
 RETURN r.id IS NOT NULL AND r.kind='v2' AND r.status='draft'
  AND NOT r.graph_sealed AND NOT r.cancel_requested
  AND q.state='reserved' AND q.expires_at>clock_timestamp()
  AND CASE p_purpose
   WHEN 'v23_training_manifest' THEN
    p_owner::text=r.config_snapshot->'v23_training'->>'profile_id' AND
    p_sha=r.config_snapshot->'v23_training'->>'manifest_sha'
   ELSE p_owner=p_run AND
    p_sha=r.config_snapshot->'v23_training'->>'snapshot_body_sha' END
  AND EXISTS(SELECT 1 FROM photo.v2_workflows w
   JOIN photo.learning_profiles p ON p.tenant_id=w.tenant_id
    AND p.project_id=w.project_id AND
    p.id::text=r.config_snapshot->'v23_training'->>'profile_id'
   JOIN photo.tenants t ON t.id=w.tenant_id
   WHERE w.tenant_id=p_tenant AND w.run_id=p_run
    AND w.workflow_type='learning' AND w.renderer_version='v2.2'
    AND t.status='active');
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_document_scope(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_document_scope(uuid,uuid,uuid,text,text) FROM PUBLIC;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.reserve_v23_training_document(p_tenant uuid,p_run uuid,
 p_owner uuid,p_purpose text,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; capacity numeric; used numeric;
 aid uuid; h bytea; fence uuid:=gen_random_uuid();
BEGIN
 IF p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL
  OR p_bytes NOT BETWEEN 2 AND (CASE WHEN p_purpose='v23_training_manifest'
   THEN 8388608 ELSE 1048576 END)
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RAISE EXCEPTION 'INVALID_V23_TRAINING_DOCUMENT' USING ERRCODE='22023';
 END IF;
 h:=public.digest(uuid_send(p_run)||convert_to(p_purpose,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 aid:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT photo.v23_training_document_scope(p_tenant,p_run,p_owner,p_purpose,p_sha)
  THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false; RETURN; END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id<>aid OR d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false; RETURN; END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
    AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256
    AND a.object_version=d.object_version) THEN
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
 VALUES(p_tenant,p_run,p_owner,p_purpose,aid,fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v23_training_document(uuid,uuid,uuid,text,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v23_training_document(uuid,uuid,uuid,text,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v23_training_document(uuid,uuid,uuid,text,text,bigint,text) TO photo_scheduler;
