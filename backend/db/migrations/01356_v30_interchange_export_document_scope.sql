-- +goose Up
-- Internal authority helper only; no storage grants or export activation here.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_export_document_scope(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,
 p_artifact uuid,p_purpose text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;frozen jsonb;request jsonb;plan jsonb;snapshot jsonb;ref jsonb;
 rid uuid;wid uuid;task uuid;sid uuid;project uuid;owner uuid;expected_id uuid;expected_body text;sha text;r photo.runs%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_tenant IS NULL OR p_actor IS NULL OR p_session IS NULL OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 120
 OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$'
 OR p_purpose IS NULL OR p_purpose NOT IN('task_params','v30_interchange_export_plan')
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608 THEN RETURN NULL;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
 AND principal_id=p_actor AND route='Interchange.Export.Pending' AND key=p_key AND request_hash=p_request_sha
 AND response_status IS NULL AND expires_at>clock_timestamp() FOR SHARE;
 IF saved IS NULL OR saved->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text OR saved->>'actor_id' IS DISTINCT FROM p_actor::text THEN RETURN NULL;END IF;
 frozen:=saved->'frozen';request:=frozen->'request';project:=(request->>'project_id')::uuid;sid:=(request->>'snapshot_id')::uuid;
 IF frozen->'schema_version' IS DISTINCT FROM '1'::jsonb OR frozen->>'request_sha256' IS DISTINCT FROM p_request_sha
 OR project IS NULL OR sid IS NULL OR request->>'format' IS NULL
 OR request->>'direction' IS DISTINCT FROM 'export' OR request->>'format' NOT IN('photoeditor_bundle','xmp','captureone_sidecar')
 OR photo.v25_lens_snapshot_ready(p_tenant,project,sid,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects p JOIN photo.tenants t ON t.id=p.tenant_id WHERE p.tenant_id=p_tenant AND p.id=project
 AND p.archived_at IS NULL AND p.download_allowed AND t.status='active') THEN RETURN NULL;END IF;
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);wid:=photo.v30_uuid5(rid,'workflow');task:=photo.v30_uuid5(wid,'v2.interchange.export:0');
 IF frozen->'identity' IS DISTINCT FROM jsonb_build_object('Run',rid,'Workflow',wid,'Task',task,'Snapshot',sid,'Params',photo.v30_uuid5(task,'interchange:params:1')) THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=rid;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'v2' OR r.created_by IS DISTINCT FROM p_actor OR r.status IS DISTINCT FROM 'draft'
 OR r.graph_sealed OR r.cancel_requested OR r.input_snapshot_id IS DISTINCT FROM sid
 OR r.config_snapshot->>'request_sha' IS DISTINCT FROM p_request_sha
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=rid AND id=wid AND project_id=project
 AND input_snapshot_id=sid AND workflow_type='interchange' AND renderer_version='v2.2') THEN RETURN NULL;END IF;
 IF frozen->>'plan_jcs' IS NULL OR frozen->>'snapshot_jcs' IS NULL
 OR octet_length(frozen->>'plan_jcs') NOT BETWEEN 2 AND 8388608 OR octet_length(frozen->>'snapshot_jcs') NOT BETWEEN 2 AND 8388608 THEN RETURN NULL;END IF;
 IF frozen->'plan' IS DISTINCT FROM jsonb_build_object('artifact_id',photo.v30_uuid5(task,'interchange:plan:1'),
 'sha256',encode(sha256(convert_to(frozen->>'plan_jcs','UTF8')),'hex')) THEN RETURN NULL;END IF;
 plan:=(frozen->>'plan_jcs')::jsonb;snapshot:=(frozen->>'snapshot_jcs')::jsonb;
 IF plan->>'snapshot_digest' IS NULL OR plan->>'snapshot_digest' !~ '^[0-9a-f]{64}$'
 OR plan->'schema_version' IS DISTINCT FROM '1'::jsonb OR plan->>'direction' IS DISTINCT FROM 'export'
 OR plan->>'project_id' IS DISTINCT FROM project::text OR plan->>'format' IS DISTINCT FROM request->>'format'
 OR snapshot->'schema_version' IS DISTINCT FROM '1'::jsonb OR snapshot->>'snapshot_id' IS DISTINCT FROM sid::text
 OR plan->>'snapshot_digest' IS DISTINCT FROM snapshot->>'snapshot_digest'
 OR plan->>'snapshot_digest' IS DISTINCT FROM r.config_snapshot->>'snapshot_digest' THEN RETURN NULL;END IF;
 IF p_purpose='task_params' THEN
  expected_id:=photo.v30_uuid5(task,'interchange:params:1');owner:=task;ref:=frozen->'params';expected_body:=frozen->>'params_jcs';
  IF octet_length(p_body)>65536 OR p_body::jsonb IS DISTINCT FROM jsonb_build_object('schema_version',1,'plan',frozen->'plan','max_report_bytes',8388608) THEN RETURN NULL;END IF;
 ELSE
  expected_id:=photo.v30_uuid5(task,'interchange:plan:1');owner:=task;ref:=frozen->'plan';expected_body:=frozen->>'plan_jcs';

 END IF;
 sha:=encode(sha256(convert_to(p_body,'UTF8')),'hex');
 IF p_artifact IS DISTINCT FROM expected_id OR expected_id IS NULL OR expected_id='00000000-0000-0000-0000-000000000000'::uuid
 OR p_body IS DISTINCT FROM expected_body OR ref IS DISTINCT FROM jsonb_build_object('artifact_id',expected_id,'sha256',sha) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('run',rid,'owner',owner,'sha',sha,'bytes',octet_length(p_body),'workflow',wid,'snapshot',sid);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_export_document_scope(uuid,uuid,uuid,text,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_export_document_scope(uuid,uuid,uuid,text,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
