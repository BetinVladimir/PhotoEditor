-- +goose Up
-- Persist original-source composition run and quota only; tasks remain absent.
-- Repeat live plan scope under tenant/usage/snapshot locks; do not activate.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v25_original_composition_draft(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,
 p_run uuid,p_workflow uuid,p_config_jcs text,p_policy_sha text,p_bytes bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant photo.tenants%ROWTYPE; usage photo.tenant_usage%ROWTYPE; old photo.runs%ROWTYPE;
 config jsonb; config_sha text; h bytea; expected uuid;
BEGIN
 IF p_run IS NULL OR p_run='00000000-0000-0000-0000-000000000000'::uuid
 OR p_workflow IS NULL OR p_workflow='00000000-0000-0000-0000-000000000000'::uuid
 OR p_config_jcs IS NULL OR octet_length(p_config_jcs) NOT BETWEEN 2 AND 1048576
 OR p_bytes IS NULL OR p_bytes NOT BETWEEN 1 AND 9007199254740991 OR coalesce(p_policy_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 SELECT * INTO tenant FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO usage FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM id FROM photo.snapshots WHERE tenant_id=p_tenant AND id=p_snapshot FOR SHARE;
 IF NOT photo.v25_lens_snapshot_ready(p_tenant,p_project,p_snapshot,p_actor,p_session) THEN RETURN false; END IF;
 config:=p_config_jcs::jsonb;config_sha:=encode(sha256(convert_to(p_config_jcs,'UTF8')),'hex');
 h:=public.digest(uuid_send(p_run)||convert_to('workflow','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected<>p_workflow THEN RETURN false; END IF;
 IF NOT photo.v25_layer_original_plan_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_workflow,config,p_bytes) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF FOUND THEN RETURN old.kind='v2' AND old.status='draft' AND NOT old.graph_sealed AND NOT old.cancel_requested
 AND old.created_by=p_actor AND old.input_snapshot_id=p_snapshot AND old.config_hash=config_sha AND old.config_snapshot=config
 AND EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.id=p_workflow AND w.policy_sha=p_policy_sha)
 AND EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=p_run
 AND q.state='reserved' AND q.bytes=p_bytes AND q.expires_at>clock_timestamp()); END IF;
 IF tenant.quotas->>'storage_bytes' IS NULL OR usage.storage_bytes::numeric+usage.reserved_bytes+p_bytes>(tenant.quotas->>'storage_bytes')::numeric
 OR usage.storage_bytes::numeric+usage.reserved_bytes+p_bytes>9007199254740991 THEN RETURN false; END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(p_run,p_tenant,'v2','draft',p_snapshot,config,config_sha,p_actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(p_workflow,p_tenant,p_project,p_run,p_snapshot,'preview','v2.2',p_policy_sha);
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',p_actor,'run',p_run,p_bytes,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_v25_original_composition_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v25_original_composition_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_v25_original_composition_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
