-- +goose Up
-- Classify only a currently authorized pending export. NULL means that the
-- pending identity or pinned Params cannot be proved; false means the exact
-- bucket has no live adoption loop. Ticket replay never calls this function.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_pending_runtime_ready(
 p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;frozen jsonb;scope jsonb;params uuid;task uuid;run uuid;bucket text;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_actor IS NULL OR p_session IS NULL OR p_key IS NULL
  OR length(p_key) NOT BETWEEN 1 AND 120 OR p_request_sha IS NULL
  OR p_request_sha !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records
 WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
  AND route='Interchange.Export.Pending' AND key=p_key
  AND request_hash=p_request_sha AND response_status IS NULL
  AND expires_at>clock_timestamp() FOR SHARE;
 IF saved IS NULL OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text
  OR saved->>'actor_id' IS DISTINCT FROM p_actor::text THEN RETURN NULL;END IF;
 frozen:=saved->'frozen';
 IF frozen->>'request_sha256' IS DISTINCT FROM p_request_sha
  OR frozen#>>'{request,direction}' IS DISTINCT FROM 'export' THEN RETURN NULL;END IF;
 run:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);
 task:=photo.v30_uuid5(photo.v30_uuid5(run,'workflow'),'v2.interchange.export:0');
 params:=photo.v30_uuid5(task,'interchange:params:1');
 scope:=photo.v30_interchange_export_document_scope(p_tenant,p_actor,p_session,
  p_key,p_request_sha,params,'task_params',frozen->>'params_jcs');
 IF scope IS NULL THEN RETURN NULL;END IF;
 SELECT a.bucket INTO bucket FROM photo.backend_documents d
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=run AND d.owner_id=task
  AND d.artifact_id=params
  AND d.purpose='task_params' AND d.state='ready'
  AND d.v30_interchange_proof IS NOT DISTINCT FROM jsonb_build_object(
   'schema_version',1,'direction','export','auth_session_id',p_session,
   'actor_id',p_actor,'key',p_key,'request_sha',p_request_sha)
  AND d.sha256=scope->>'sha' AND a.sha256=d.sha256 AND a.state='ready'
  AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF bucket IS NULL THEN RETURN NULL;END IF;
 RETURN photo.v30_export_runtime_ready(bucket);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_export_pending_runtime_ready(uuid,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_pending_runtime_ready(uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v30_export_pending_runtime_ready(uuid,uuid,uuid,text,text)
 TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
