-- +goose Up
-- Dedicated API activation. The API must supply the exact task from its private
-- native/resource-qualified preparation receipt; SQL rechecks live authority.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v30_interchange_export(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_task jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;wid uuid;tid uuid;ticket jsonb;receipt photo.idempotency_records%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;expected jsonb;project uuid;snapshot uuid;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 120
 OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_task) IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);wid:=photo.v30_uuid5(rid,'workflow');tid:=photo.v30_uuid5(wid,'v2.interchange.export:0');
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=rid FOR UPDATE;
 SELECT w.project_id,w.input_snapshot_id INTO project,snapshot FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.id=wid AND w.run_id=rid AND w.workflow_type='interchange' AND w.renderer_version='v2.2';
 IF r.id IS NULL OR r.kind<>'v2' OR r.created_by IS DISTINCT FROM p_actor OR r.config_snapshot->>'request_sha' IS DISTINCT FROM p_request_sha
 OR project IS NULL OR photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) IS DISTINCT FROM true
 OR photo.v25_lens_snapshot_ready(p_tenant,project,snapshot,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=project AND archived_at IS NULL AND download_allowed) THEN RETURN NULL;END IF;
 ticket:=jsonb_build_object('run_id',rid,'workflow_id',wid,'state','queued');
 SELECT * INTO receipt FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor AND route='Interchange.Export.Ticket' AND key=p_key FOR UPDATE;
 IF FOUND THEN
  IF receipt.request_hash IS DISTINCT FROM p_request_sha OR receipt.expires_at<=clock_timestamp() OR receipt.response_status IS DISTINCT FROM 202
  OR receipt.response_body IS DISTINCT FROM ticket OR receipt.resource_id IS DISTINCT FROM wid OR NOT r.graph_sealed OR r.status='draft' THEN RETURN NULL;END IF;
  RETURN jsonb_build_object('ticket',ticket,'replayed',true);
 END IF;
 IF photo.v30_interchange_export_draft_ready(p_tenant,p_actor,p_session,p_key,p_request_sha,p_task->>'input_digest') IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid AND id=tid FOR UPDATE;
 expected:=jsonb_build_object('id',tid,'node_key','v2.interchange.export:0','operation','v2.interchange.export','operation_version','1','resource_class','cpu',
 'params_artifact_id',t.params_artifact_id,'params_sha',(SELECT sha256 FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=rid AND owner_id=tid AND purpose='task_params' AND artifact_id=t.params_artifact_id AND state='ready'),
 'input_digest',t.input_digest,'execution_profile',t.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha');
 IF p_task IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 -- Bypass only the export-specific generic seal denial. Every earlier consent,
 -- registry, operation/resource/closure and quota gate remains in the chain.
 IF photo.seal_v2_graph_pre_v30_export(p_tenant,rid) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'INTERCHANGE_EXPORT_ACTIVATION_REFUSED' USING ERRCODE='23514';
 END IF;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,response_status,response_body,resource_id,expires_at)
 VALUES(p_tenant,'user',p_actor,'Interchange.Export.Ticket',p_key,p_request_sha,202,ticket,wid,clock_timestamp()+interval '24 hours');
 RETURN jsonb_build_object('ticket',ticket,'replayed',false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.activate_v30_interchange_export(uuid,uuid,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_interchange_export(uuid,uuid,uuid,text,text,jsonb) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.activate_v30_interchange_export(uuid,uuid,uuid,text,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
