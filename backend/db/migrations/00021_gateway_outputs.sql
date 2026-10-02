-- +goose Up
-- Output policies are privileged operation contracts. No policy means deny.
CREATE TABLE photo.operation_output_policies (
 operation text NOT NULL,version text NOT NULL,role text NOT NULL CHECK(role ~ '^[a-z][a-z0-9_-]{0,63}$'),
 media_types jsonb NOT NULL CHECK(jsonb_typeof(media_types)='array'),
 max_bytes bigint NOT NULL CHECK(max_bytes BETWEEN 0 AND 9007199254740991),
 max_count integer NOT NULL CHECK(max_count BETWEEN 1 AND 128),
 PRIMARY KEY(operation,version,role),
 FOREIGN KEY(operation,version) REFERENCES photo.operation_registry(operation,version)
);
GRANT SELECT ON photo.operation_output_policies TO photo_fn_owner;
CREATE TABLE photo.output_uploads (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL,task_id uuid NOT NULL,
 attempt integer NOT NULL CHECK(attempt BETWEEN 1 AND 6),fence bigint NOT NULL CHECK(fence>0),
 worker_id uuid NOT NULL,certificate_sha256 text NOT NULL CHECK(certificate_sha256 ~ '^[0-9a-f]{64}$'),
 idempotency_key uuid NOT NULL,artifact_id uuid NOT NULL UNIQUE,role text NOT NULL,
 bucket text NOT NULL,object_key text NOT NULL,sha256 text NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
 bytes bigint NOT NULL CHECK(bytes BETWEEN 0 AND 9007199254740991),media_type text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','ready','cleaning','cleaned')),
 object_version text,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),completed_at timestamptz,
 UNIQUE(tenant_id,task_id,attempt,fence,idempotency_key),UNIQUE(bucket,object_key),
 FOREIGN KEY(tenant_id,task_id) REFERENCES photo.tasks(tenant_id,id)
);
ALTER TABLE photo.output_uploads ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.output_uploads FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.output_uploads TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.output_uploads TO photo_fn_owner;
-- +goose StatementBegin
CREATE FUNCTION photo.begin_output(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text,
 p_key uuid,p_role text,p_bytes bigint,p_sha text,p_media text,p_bucket text)
RETURNS TABLE(outcome text,upload_id uuid,artifact_id uuid,object_key text,sha256 text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; policy photo.operation_output_policies%ROWTYPE; old photo.output_uploads%ROWTYPE;
 uid uuid:=gen_random_uuid(); aid uuid:=gen_random_uuid(); target text; maximum numeric; used numeric; budget numeric;
BEGIN
 IF p_key IS NULL OR p_key='00000000-0000-0000-0000-000000000000'::uuid OR p_role IS NULL OR p_role !~ '^[a-z][a-z0-9_-]{0,63}$'
 OR p_bytes IS NULL OR p_bytes<0 OR p_bytes>9007199254740991 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR p_media IS NULL OR p_media !~ '^[a-z0-9.+-]+/[a-z0-9.+-]+$' OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
 RAISE EXCEPTION 'INVALID_UPLOAD' USING ERRCODE='22023'; END IF;
 t:=photo.lock_task(p_task);
 IF t.id IS NULL OR NOT EXISTS(SELECT FROM photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate)) THEN
 RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,NULL::text,NULL::text; RETURN; END IF;
 SELECT * INTO old FROM photo.output_uploads u WHERE u.tenant_id=t.tenant_id AND u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.idempotency_key=p_key FOR UPDATE;
 IF FOUND THEN
  IF old.worker_id<>p_worker OR old.certificate_sha256<>p_certificate OR old.role<>p_role OR old.bytes<>p_bytes OR old.sha256<>p_sha OR old.media_type<>p_media OR old.bucket<>p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,NULL::text,NULL::text;
  ELSIF old.state='ready' THEN RETURN QUERY SELECT 'replayed',old.id,old.artifact_id,old.object_key,old.sha256;
  ELSE RETURN QUERY SELECT 'busy',NULL::uuid,NULL::uuid,NULL::text,NULL::text;
  END IF; RETURN;
 END IF;
 SELECT * INTO policy FROM photo.operation_output_policies p WHERE p.operation=t.operation AND p.version=t.operation_version AND p.role=p_role;
 IF NOT FOUND OR NOT(policy.media_types ? p_media) OR p_bytes>policy.max_bytes THEN
 RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,NULL::text,NULL::text; RETURN; END IF;
 IF (SELECT count(*) FROM photo.output_uploads u WHERE u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence)>=128
 OR (SELECT count(*) FROM photo.output_uploads u WHERE u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.role=p_role)>=policy.max_count THEN
 RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,NULL::text,NULL::text; RETURN; END IF;
 budget:=(t.execution_profile->'resource_estimate'->>'output_bytes')::numeric;
 SELECT coalesce(sum(u.bytes),0) INTO used FROM photo.output_uploads u WHERE u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence;
 IF budget IS NULL OR budget<0 OR used+p_bytes>budget THEN
 RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,NULL::text,NULL::text; RETURN; END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO maximum FROM photo.tenants WHERE id=t.tenant_id;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=t.tenant_id;
 IF maximum IS NULL OR used IS NULL OR used+p_bytes>maximum OR used+p_bytes>9007199254740991 THEN
 RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,NULL::text,NULL::text; RETURN; END IF;
 target:='tenants/'||t.tenant_id||'/tasks/'||t.id||'/attempts/'||p_attempt||'/fences/'||p_fence||'/'||aid;
 INSERT INTO photo.output_uploads(id,tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type)
 VALUES(uid,t.tenant_id,t.id,p_attempt,p_fence,p_worker,p_certificate,p_key,aid,p_role,p_bucket,target,p_sha,p_bytes,p_media);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=t.tenant_id;
 RETURN QUERY SELECT 'created',uid,aid,target,p_sha;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,text) TO photo_gateway;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_output(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text,
 p_upload uuid,p_version text,p_bytes bigint,p_sha text,p_media text)
RETURNS TABLE(outcome text,artifact_id uuid,sha256 text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; u photo.output_uploads%ROWTYPE;
BEGIN
 IF p_version IS NULL OR p_version IN ('','null') OR length(p_version)>1024 THEN RAISE EXCEPTION 'INVALID_OBJECT_VERSION' USING ERRCODE='22023'; END IF;
 t:=photo.lock_task(p_task);
 IF t.id IS NULL OR NOT EXISTS(SELECT FROM photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate)) THEN
 RETURN QUERY SELECT 'stale',NULL::uuid,NULL::text; RETURN; END IF;
 SELECT * INTO u FROM photo.output_uploads WHERE id=p_upload AND tenant_id=t.tenant_id AND task_id=t.id FOR UPDATE;
 IF NOT FOUND OR u.attempt<>p_attempt OR u.fence<>p_fence OR u.worker_id<>p_worker OR u.certificate_sha256<>p_certificate
 OR u.bytes IS DISTINCT FROM p_bytes OR u.sha256 IS DISTINCT FROM p_sha OR u.media_type IS DISTINCT FROM p_media THEN
 RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text; RETURN; END IF;
 IF u.state='ready' THEN
  IF u.object_version=p_version THEN RETURN QUERY SELECT 'replayed',u.artifact_id,u.sha256;
  ELSE RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text; END IF; RETURN;
 END IF;
 IF u.state<>'pending' THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text; RETURN; END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,created_by_task_id,produced_attempt,produced_fence,verified_at,output_role)
 VALUES(u.artifact_id,u.tenant_id,u.bucket,u.object_key,p_version,u.sha256,u.bytes,u.media_type,'ready',t.id,p_attempt,p_fence,clock_timestamp(),u.role);
 UPDATE photo.output_uploads SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=u.id;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-u.bytes,storage_bytes=storage_bytes+u.bytes WHERE tenant_id=t.tenant_id AND reserved_bytes>=u.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'UPLOAD_RESERVATION_INCONSISTENT' USING ERRCODE='23514'; END IF;
 RETURN QUERY SELECT 'committed',u.artifact_id,u.sha256;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text) TO photo_gateway;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
