-- +goose Up
-- One physically retained report belonging to an ancestor's canonical
-- ImageResult root, beneath the active Delivery signer delegation.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_parent_report(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_report uuid,p_report_sha text,p_bucket text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;result jsonb;producer uuid;e jsonb;
BEGIN
 IF p_report_sha!~'^[0-9a-f]{64}$' OR
  length(p_bucket)<3 OR length(p_bucket)>63 THEN RETURN NULL;END IF;
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN NULL;END IF;
 SELECT a.created_by_task_id INTO producer FROM photo.artifacts a
 WHERE a.tenant_id=(source->>'tenant_id')::uuid AND a.id=p_report
  AND a.sha256=p_report_sha AND a.bucket=p_bucket;
 IF producer IS NULL THEN RETURN NULL;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,producer);
 IF e IS NULL OR jsonb_array_length(e->'outputs')<>1
  THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('task',t.id,'root',root.artifact_id,
  'root_sha',root.sha256,'bucket',a.bucket,'key',a.object_key,
  'version',a.object_version,'sha256',a.sha256,'bytes',a.bytes,
  'media_type',a.media_type)
 INTO result FROM photo.artifacts a
 JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id
  AND z.id=t.canonical_result_id
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 JOIN photo.v2_artifact_links report ON report.tenant_id=w.tenant_id
  AND report.workflow_id=w.id AND report.project_id=w.project_id
  AND report.artifact_id=a.id AND report.sha256=a.sha256
 JOIN photo.v2_artifact_links root ON root.tenant_id=w.tenant_id
  AND root.workflow_id=w.id AND root.project_id=w.project_id
 WHERE a.tenant_id=(source->>'tenant_id')::uuid AND a.id=p_report
  AND a.sha256=p_report_sha AND a.bucket=p_bucket
  AND w.project_id=(source->>'project_id')::uuid
  AND w.workflow_type='preview' AND w.renderer_version='v2.2'
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version NOT IN('','null') AND a.media_type='application/json'
  AND a.output_role='report' AND a.bytes BETWEEN 2 AND 1048576
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND t.id=producer AND t.state='succeeded' AND NOT t.cancel_requested
  AND t.attempt=(e->>'attempt')::integer AND t.fencing_token=(e->>'fence')::bigint
  AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
  AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  AND report.purpose=encode(sha256(convert_to(
   'tasks/'||t.id||'/$/report','UTF8')),'hex')
  AND root.ordinal=0 AND root.purpose=encode(sha256(convert_to(
   'tasks/'||t.id||'/$','UTF8')),'hex')
  AND z.outputs=jsonb_build_array(jsonb_build_object(
   'artifact_id',root.artifact_id::text,'sha256',root.sha256))
 FOR SHARE OF a,t,z;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_parent_report(uuid,uuid,integer,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_parent_report(uuid,uuid,integer,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_parent_report(uuid,uuid,integer,uuid,uuid,text,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
