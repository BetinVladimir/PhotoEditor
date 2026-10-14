-- +goose Up
-- Resolve only the snapshot frozen by the currently admitted export request.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_snapshot_authority(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE context jsonb;proof jsonb;saved jsonb;reference jsonb;actor uuid;config jsonb;snapshot uuid;
BEGIN
 context:=photo.v30_interchange_export_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest);
 IF context IS NULL THEN RETURN NULL;END IF;
 SELECT r.created_by,r.config_snapshot,r.input_snapshot_id,d.v30_interchange_proof INTO actor,config,snapshot,proof
 FROM photo.runs r JOIN photo.backend_documents d ON d.tenant_id=r.tenant_id AND d.run_id=r.id
 AND d.owner_id=p_task AND d.artifact_id=p_params AND d.sha256=p_sha AND d.purpose='task_params' AND d.state='ready'
 WHERE r.tenant_id=p_tenant AND r.id=p_run;
 SELECT response_body INTO saved FROM photo.idempotency_records
 WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=actor AND route='Interchange.Export.Pending'
 AND key=proof->>'key' AND request_hash=proof->>'request_sha' AND response_status IS NULL AND expires_at>clock_timestamp() FOR SHARE;
 reference:=saved->'frozen'->'snapshot';
 IF jsonb_typeof(reference) IS DISTINCT FROM 'object'
 OR NOT reference ?& ARRAY['artifact_id','sha256'] OR reference-'artifact_id'-'sha256'<>'{}'::jsonb
 OR reference IS DISTINCT FROM config->'snapshot_document'
 OR NOT EXISTS(SELECT 1 FROM photo.snapshots s JOIN photo.artifacts a ON a.tenant_id=s.tenant_id AND a.id=s.artifact_id
 JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.sha256=a.sha256
 WHERE s.tenant_id=p_tenant AND s.id=snapshot AND s.kind='v2' AND s.expires_at IS NULL
 AND a.id=(reference->>'artifact_id')::uuid AND a.sha256=reference->>'sha256'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND i.task_id=p_task AND i.contract='InputSnapshot') THEN RETURN NULL;END IF;
 RETURN reference;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_export_snapshot_authority(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_snapshot_authority(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_snapshot_authority(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
