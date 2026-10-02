-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.stage_v25_lens_task(p_tenant uuid,p_run uuid,p_task uuid,p_ordinal integer,
 p_params uuid,p_sha text,p_digest text,p_profile jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; old photo.tasks%ROWTYPE;
 node jsonb; h bytea; expected uuid; key text;
BEGIN
 IF p_task IS NULL OR p_params IS NULL OR p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 4999
 OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(p_digest,'') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_profile) IS DISTINCT FROM 'object' THEN RETURN false; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF r.id IS NULL OR w.id IS NULL OR NOT photo.v25_lens_document_scope(p_tenant,p_run,p_task,'v25_lens_params',p_sha)
 THEN RETURN false; END IF;
 key:='optical.lens:'||p_ordinal;
 h:=public.digest(uuid_send(w.id)||convert_to(key,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected<>p_task THEN RETURN false; END IF;
 SELECT value INTO node FROM jsonb_array_elements(r.config_snapshot->'v25_lens'->'nodes')
 WHERE value->>'task_id'=p_task::text;
 IF p_profile IS DISTINCT FROM node->'profile' OR p_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR p_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a
 ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.object_version=d.object_version
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_task AND d.purpose='v25_lens_params'
 AND d.state='ready' AND d.artifact_id=p_params AND d.sha256=p_sha AND a.state='ready' AND a.verified_at IS NOT NULL
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND photo.v25_lens_document_bytes(p_tenant,p_run,p_task,d.purpose,d.sha256,d.bytes))
 OR NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a
 ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.object_version=d.object_version
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_task AND d.purpose='v25_lens_original_binding'
 AND d.state='ready' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND photo.v25_lens_document_bytes(p_tenant,p_run,p_task,d.purpose,d.sha256,d.bytes)) THEN RETURN false; END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF FOUND THEN RETURN old.run_id=p_run AND old.node_key=key AND old.operation='optical.lens'
 AND old.operation_version='1' AND old.resource_class='cpu' AND old.params_artifact_id=p_params
 AND old.input_digest=p_digest AND (old.execution_profile=p_profile OR (photo.v2_input_closure_ready(old.id)
 AND (old.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=p_profile)) AND old.state='blocked'
 AND old.attempt=0 AND NOT old.cancel_requested; END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(p_task,p_tenant,p_run,key,'optical.lens','1','cpu','[]'::jsonb,p_params,p_digest,p_profile);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.stage_v25_lens_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v25_lens_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v25_lens_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
