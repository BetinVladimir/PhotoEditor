-- +goose Up
-- Persist prepublication intent and quota atomically; no task/queue activation.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v28_local_portrait_draft(p_config_jcs text,p_policy_sha text,p_bytes bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
#variable_conflict use_variable
DECLARE config jsonb;request jsonb;config_sha text;tenant photo.tenants%ROWTYPE;
 usage photo.tenant_usage%ROWTYPE;old photo.runs%ROWTYPE;tenant_id uuid;project_id uuid;snapshot_id uuid;
 actor_id uuid;run_id uuid;workflow_id uuid;minimum numeric;
BEGIN
 IF p_config_jcs IS NULL OR octet_length(p_config_jcs) NOT BETWEEN 2 AND 1048576
  OR coalesce(p_policy_sha,'') !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR p_bytes NOT BETWEEN 1 AND 9007199254740991 THEN RETURN false;END IF;
 config:=p_config_jcs::jsonb;request:=config->'v28_local_portrait';
 IF jsonb_typeof(config) IS DISTINCT FROM 'object'
  OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(config) k) IS DISTINCT FROM ARRAY['input_snapshot_id','project_id','v28_local_portrait']::text[]
  OR jsonb_typeof(request) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 tenant_id:=(request->>'tenant_id')::uuid;project_id:=(request->>'project_id')::uuid;snapshot_id:=(request->>'snapshot_id')::uuid;
 actor_id:=(request->>'actor_id')::uuid;run_id:=(request->>'run_id')::uuid;workflow_id:=(request->>'workflow_id')::uuid;
 IF config->>'project_id' IS DISTINCT FROM project_id::text OR config->>'input_snapshot_id' IS DISTINCT FROM snapshot_id::text THEN RETURN false;END IF;
 SELECT * INTO tenant FROM photo.tenants WHERE id=tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO usage FROM photo.tenant_usage WHERE photo.tenant_usage.tenant_id=tenant_id FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.snapshots WHERE photo.snapshots.tenant_id=tenant_id AND id=snapshot_id FOR SHARE;
 IF NOT FOUND OR photo.v28_local_owner_request_scope(request) IS DISTINCT FROM true THEN RETURN false;END IF;
 -- Full output envelope plus one bounded Params, one chunk and one manifest.
 minimum:=(request->'intent'->>'reserved_bytes')::numeric+65536+2097152;
 IF p_bytes<minimum THEN RETURN false;END IF;
 config_sha:=encode(sha256(convert_to(p_config_jcs,'UTF8')),'hex');
 SELECT * INTO old FROM photo.runs WHERE photo.runs.tenant_id=tenant_id AND id=run_id FOR UPDATE;
 IF FOUND THEN RETURN old.kind='v2' AND old.status='draft' AND NOT old.graph_sealed AND NOT old.cancel_requested
  AND old.created_by=actor_id AND old.input_snapshot_id=snapshot_id AND old.config_hash=config_sha AND old.config_snapshot=config
  AND EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=tenant_id AND w.run_id=run_id AND w.id=workflow_id
   AND w.project_id=project_id AND w.input_snapshot_id=snapshot_id AND w.workflow_type='preview' AND w.renderer_version='v2.2' AND w.policy_sha=p_policy_sha)
  AND EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=tenant_id AND q.holder_type='run' AND q.holder_id=run_id
   AND q.state='reserved' AND q.bytes=p_bytes AND q.expires_at>clock_timestamp());END IF;
 IF tenant.quotas->>'storage_bytes' IS NULL OR usage.storage_bytes::numeric+usage.reserved_bytes+p_bytes>(tenant.quotas->>'storage_bytes')::numeric
  OR usage.storage_bytes::numeric+usage.reserved_bytes+p_bytes>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(run_id,tenant_id,'v2','draft',snapshot_id,config,config_sha,actor_id,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(workflow_id,tenant_id,project_id,run_id,snapshot_id,'preview','v2.2',p_policy_sha);
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES(tenant_id,'user',actor_id,'run',run_id,p_bytes,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE photo.tenant_usage.tenant_id=tenant_id;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_v28_local_portrait_draft(text,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v28_local_portrait_draft(text,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_v28_local_portrait_draft(text,text,bigint) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
