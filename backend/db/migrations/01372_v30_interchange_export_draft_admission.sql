-- +goose Up
-- Current database authority for draft admission. This is not native resource
-- qualification and deliberately cannot seal a graph or produce a ticket.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_export_draft_ready(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_input_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;frozen jsonb;rid uuid;wid uuid;tid uuid;params uuid;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;d photo.backend_documents%ROWTYPE;scope jsonb;profile jsonb;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 120
 OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$' OR p_input_digest IS NULL OR p_input_digest !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='Interchange.Export.Pending' AND key=p_key AND request_hash=p_request_sha AND response_status IS NULL AND expires_at>clock_timestamp() FOR UPDATE;
 IF saved IS NULL OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text OR saved->>'actor_id' IS DISTINCT FROM p_actor::text THEN RETURN false;END IF;
 frozen:=saved->'frozen';rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);wid:=photo.v30_uuid5(rid,'workflow');
 tid:=photo.v30_uuid5(wid,'v2.interchange.export:0');params:=photo.v30_uuid5(tid,'interchange:params:1');
 IF frozen->>'request_sha256' IS DISTINCT FROM p_request_sha OR frozen#>>'{request,direction}' IS DISTINCT FROM 'export' THEN RETURN false;END IF;
 scope:=photo.v30_interchange_export_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,params,'task_params',frozen->>'params_jcs');
 IF scope IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=rid FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid AND id=tid FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor
 OR t.id IS NULL OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested OR t.operation<>'v2.interchange.export' OR t.operation_version<>'1'
 OR t.resource_class<>'cpu' OR t.params_artifact_id IS DISTINCT FROM params OR t.input_digest IS DISTINCT FROM p_input_digest
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid)<>1
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=rid)
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=rid)
 OR photo.v2_input_closure_ready(tid) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=rid AND owner_id=tid AND purpose='task_params'
 AND artifact_id=params AND sha256=scope->>'sha' AND bytes=(scope->>'bytes')::bigint AND state='ready' FOR SHARE;
 IF d.id IS NULL OR d.v30_interchange_proof IS DISTINCT FROM jsonb_build_object('schema_version',1,'direction','export','auth_session_id',p_session,'actor_id',p_actor,'key',p_key,'request_sha',p_request_sha) THEN RETURN false;END IF;
 profile:=t.execution_profile;
 IF profile->>'backend' IS DISTINCT FROM 'cpu' OR profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR t.inputs IS DISTINCT FROM (jsonb_build_array(profile->'v2_input_manifest')||jsonb_build_array(frozen->'params',frozen->'plan',frozen->'snapshot')) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=tid AND contract='Params' AND artifact_id=params AND sha256=scope->>'sha')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=tid AND contract='InterchangePlan' AND artifact_id=(frozen#>>'{plan,artifact_id}')::uuid AND sha256=frozen#>>'{plan,sha256}')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=tid AND contract='InputSnapshot' AND artifact_id=(frozen#>>'{snapshot,artifact_id}')::uuid AND sha256=frozen#>>'{snapshot,sha256}') THEN RETURN false;END IF;
 PERFORM a.id FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
 WHERE i.tenant_id=p_tenant AND i.task_id=tid ORDER BY a.id FOR SHARE OF a;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=tid AND (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL
 OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=rid
 AND principal_type='user' AND principal_id=p_actor AND state='reserved' AND bytes=(profile#>>'{resource_estimate,output_bytes}')::bigint AND expires_at>clock_timestamp());
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_interchange_export_draft_ready(uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_export_draft_ready(uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_export_draft_ready(uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
