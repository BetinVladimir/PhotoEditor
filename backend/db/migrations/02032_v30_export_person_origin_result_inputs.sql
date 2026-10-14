-- +goose Up
-- Result-phase read of exact signed receipts declared before export task seal.
-- The physical JSON signature is checked by the API's process trust policy;
-- this function pins the canonical succeeded result and immutable object row.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_person_origin_result_inputs(
 p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_root uuid,p_root_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;tid uuid;count_receipts integer;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_root IS NULL OR coalesce(p_root_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_interchange_export_output(p_tenant,p_actor,p_session,p_key,p_request_sha,
  p_root,p_root_sha,'InterchangeResult');
 IF NOT FOUND THEN RETURN;END IF;
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);
 tid:=photo.v30_uuid5(photo.v30_uuid5(rid,'workflow'),'v2.interchange.export:0');
 SELECT count(*) INTO count_receipts FROM photo.v2_task_inputs i
 WHERE i.tenant_id=p_tenant AND i.task_id=tid AND i.contract='PersonOriginReceipt';
 IF count_receipts>5000 THEN RAISE EXCEPTION 'V30_EXPORT_PERSON_ORIGIN_LIMIT' USING ERRCODE='23514';END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i
  LEFT JOIN photo.backend_documents d ON d.tenant_id=i.tenant_id AND d.run_id=rid
   AND d.owner_id=i.artifact_id AND d.artifact_id=i.artifact_id AND d.purpose='v30_person_origin_receipt'
  LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
  WHERE i.tenant_id=p_tenant AND i.task_id=tid AND i.contract='PersonOriginReceipt'
   AND (d.id IS NULL OR d.state IS DISTINCT FROM 'ready' OR d.sha256 IS DISTINCT FROM i.sha256
    OR d.bytes NOT BETWEEN 2 AND 16384 OR d.object_version IS NULL OR d.object_version IN('','null')
    OR a.id IS NULL OR a.sha256 IS DISTINCT FROM d.sha256 OR a.bytes IS DISTINCT FROM d.bytes
    OR a.bucket IS DISTINCT FROM d.bucket OR a.object_key IS DISTINCT FROM
     'tenants/'||p_tenant||'/runs/'||rid||'/documents/'||i.artifact_id
    OR a.object_version IS DISTINCT FROM d.object_version OR a.media_type IS DISTINCT FROM 'application/json'
    OR a.output_role IS DISTINCT FROM 'v30_person_origin_receipt' OR a.state IS DISTINCT FROM 'ready'
    OR a.verified_at IS NULL OR a.expires_at<=clock_timestamp())) THEN
  RAISE EXCEPTION 'V30_EXPORT_PERSON_ORIGIN_STALE' USING ERRCODE='23514';END IF;
 RETURN QUERY SELECT i.artifact_id,i.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.v2_task_inputs i JOIN photo.backend_documents d ON d.tenant_id=i.tenant_id
  AND d.run_id=rid AND d.owner_id=i.artifact_id AND d.artifact_id=i.artifact_id
  AND d.purpose='v30_person_origin_receipt' AND d.state='ready'
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
 WHERE i.tenant_id=p_tenant AND i.task_id=tid AND i.contract='PersonOriginReceipt'
 ORDER BY i.artifact_id FOR SHARE OF i,d,a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_person_origin_result_inputs(uuid,uuid,uuid,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_person_origin_result_inputs(uuid,uuid,uuid,text,text,uuid,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_person_origin_result_inputs(uuid,uuid,uuid,text,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
