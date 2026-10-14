-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_document_scope(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,
 p_artifact uuid,p_purpose text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;request jsonb;rid uuid;wid uuid;task uuid;sid uuid;project uuid;owner uuid;expected_id uuid;
 r photo.runs%ROWTYPE;body jsonb;expected jsonb;sha text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536
 OR p_purpose NOT IN('task_params','v30_interchange_snapshot') THEN RETURN NULL;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
 AND principal_id=p_actor AND route='Interchange.Parse.Pending' AND key=p_key AND request_hash=p_request_sha
 AND response_status IS NULL AND expires_at>clock_timestamp() FOR SHARE;
 IF saved IS NULL OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR saved->>'actor_id' IS DISTINCT FROM p_actor::text OR saved->>'request_sha' IS DISTINCT FROM p_request_sha THEN RETURN NULL;END IF;
 request:=saved->'request';project:=(request->>'project_id')::uuid;
 IF request->>'direction' IS DISTINCT FROM 'import' OR request->>'format' IS DISTINCT FROM 'photoeditor_bundle'
 OR saved->'evidence'->'ref' IS DISTINCT FROM request->'bundle'
 OR photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=project AND archived_at IS NULL AND download_allowed) THEN RETURN NULL;END IF;
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);wid:=photo.v30_uuid5(rid,'workflow');
 task:=photo.v30_uuid5(wid,'v2.interchange.parse:0');sid:=photo.v30_uuid5(wid,'interchange:artifact-snapshot:1');
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=rid;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'v2' OR r.created_by IS DISTINCT FROM p_actor
 OR r.status IS DISTINCT FROM 'draft' OR r.graph_sealed OR r.cancel_requested
 OR r.config_snapshot->>'request_sha' IS DISTINCT FROM p_request_sha
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=rid AND id=wid AND project_id=project AND workflow_type='interchange') THEN RETURN NULL;END IF;
 body:=p_body::jsonb;sha:=encode(sha256(convert_to(p_body,'UTF8')),'hex');
 IF p_purpose='task_params' THEN
  expected_id:=photo.v30_uuid5(task,'interchange:params:1');owner:=task;
  expected:=jsonb_build_object('schema_version',1,'request',request,'max_expanded_bytes',saved->'limits'->'expanded_bytes','max_files',saved->'limits'->'files');
 ELSE
  expected_id:=photo.v30_uuid5(wid,'interchange:artifact-snapshot-document:1');owner:=rid;
  expected:=jsonb_build_object('schema_version',1,'snapshot_id',sid,'snapshot_digest',r.config_snapshot->>'snapshot_digest',
  'project_id',project,'items',jsonb_build_array(jsonb_build_object('ordinal',0,'artifact',request->'bundle','contract','InterchangeArchive','owner_id',NULL)));
  IF r.config_snapshot->'snapshot_document' IS DISTINCT FROM jsonb_build_object('artifact_id',expected_id,'sha256',sha) THEN RETURN NULL;END IF;
 END IF;
 IF p_artifact IS DISTINCT FROM expected_id OR body IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('run',rid,'owner',owner,'sha',sha,'bytes',octet_length(p_body));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_document_scope(uuid,uuid,uuid,text,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_document_scope(uuid,uuid,uuid,text,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
