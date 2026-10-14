-- +goose Up
-- Recover the original server-frozen export plan for scheduler qualification.
-- A leased read does not activate or adopt any task and is never user input.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_frozen(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;saved jsonb;frozen jsonb;
BEGIN
 IF photo.v30_export_adoption_claim_current(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow AND run_id=p_run FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 SELECT response_body INTO saved FROM photo.idempotency_records
  WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
   AND route='Interchange.Export.Pending' AND key=p_key AND request_hash=p_request_sha
   AND response_status IS NULL AND expires_at>clock_timestamp() FOR SHARE;
 IF w.id IS NULL OR r.id IS NULL OR saved IS NULL
  OR w.v30_export_adoption_token IS DISTINCT FROM p_token
  OR (w.v30_export_adoption_retry_after>clock_timestamp()) IS DISTINCT FROM true
  OR r.created_by IS DISTINCT FROM p_actor OR r.status IS DISTINCT FROM 'running'
  OR r.cancel_requested OR NOT r.graph_sealed
  OR r.config_snapshot->>'request_sha' IS DISTINCT FROM p_request_sha
  OR photo.v30_export_adoption_source_ready(p_tenant,w.project_id,w.input_snapshot_id,p_actor) IS DISTINCT FROM true
  OR saved->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text
  OR saved->>'actor_id' IS DISTINCT FROM p_actor::text THEN RETURN NULL;END IF;
 frozen:=saved->'frozen';
 IF jsonb_typeof(frozen) IS DISTINCT FROM 'object'
  OR frozen->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR frozen->>'request_sha256' IS DISTINCT FROM p_request_sha
  OR frozen->'request'->>'direction' IS DISTINCT FROM 'export'
  OR frozen->'request'->>'project_id' IS DISTINCT FROM w.project_id::text
  OR frozen->'request'->>'snapshot_id' IS DISTINCT FROM w.input_snapshot_id::text
  OR frozen->'identity' IS DISTINCT FROM jsonb_build_object(
   'Run',p_run,'Workflow',p_workflow,'Task',p_task,'Snapshot',w.input_snapshot_id,
   'Params',photo.v30_uuid5(p_task,'interchange:params:1')) THEN RETURN NULL;END IF;
 RETURN frozen;
END $$;
ALTER FUNCTION photo.v30_export_adoption_frozen(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_frozen(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_frozen(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
