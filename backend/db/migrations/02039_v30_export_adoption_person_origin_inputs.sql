-- +goose Up
-- Scheduler reads only receipts sealed into the exact canonical export task.
-- This does not grant access to arbitrary tenant documents or ZIP members.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_person_origin_inputs(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,
 object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE receipt_count integer;
BEGIN
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN;END IF;
 SELECT count(*) INTO receipt_count FROM photo.v2_task_inputs i
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='PersonOriginReceipt';
 IF receipt_count>5000 THEN RAISE EXCEPTION 'V30_EXPORT_PERSON_ORIGIN_LIMIT' USING ERRCODE='23514';END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i
  LEFT JOIN photo.backend_documents d ON d.tenant_id=i.tenant_id AND d.run_id=p_run
   AND d.owner_id=i.artifact_id AND d.artifact_id=i.artifact_id AND d.purpose='v30_person_origin_receipt'
  LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='PersonOriginReceipt'
   AND (d.id IS NULL OR d.state IS DISTINCT FROM 'ready' OR d.sha256 IS DISTINCT FROM i.sha256
    OR d.bytes NOT BETWEEN 2 AND 16384 OR d.object_version IS NULL OR d.object_version IN('','null')
    OR a.id IS NULL OR a.sha256 IS DISTINCT FROM d.sha256 OR a.bytes IS DISTINCT FROM d.bytes
    OR a.bucket IS DISTINCT FROM d.bucket OR a.object_key IS DISTINCT FROM
     'tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||i.artifact_id
    OR a.object_version IS DISTINCT FROM d.object_version OR a.media_type IS DISTINCT FROM 'application/json'
    OR a.output_role IS DISTINCT FROM 'v30_person_origin_receipt' OR a.state IS DISTINCT FROM 'ready'
    OR a.verified_at IS NULL OR a.expires_at<=clock_timestamp())) THEN
  RAISE EXCEPTION 'V30_EXPORT_PERSON_ORIGIN_STALE' USING ERRCODE='23514';END IF;
 RETURN QUERY SELECT i.artifact_id,i.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.v2_task_inputs i JOIN photo.backend_documents d ON d.tenant_id=i.tenant_id
  AND d.run_id=p_run AND d.owner_id=i.artifact_id AND d.artifact_id=i.artifact_id
  AND d.purpose='v30_person_origin_receipt' AND d.state='ready'
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='PersonOriginReceipt'
 ORDER BY i.artifact_id FOR SHARE OF i,d,a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_person_origin_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_person_origin_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_person_origin_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
