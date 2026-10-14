-- +goose Up
-- Canonical final export-task evidence for delegated V31 lineage. Ancestor
-- traversal and operation-specific claims require separate scoped readers.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_final_task_evidence(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;result jsonb;
BEGIN
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('task',t.id,'run',t.run_id,'result',z.id,
  'attempt',t.attempt,'fence',t.fencing_token,'operation',t.operation,
  'version',t.operation_version,'input_digest',t.input_digest,
  'result_digest',z.result_digest,
  'params',jsonb_build_object('artifact_id',p.id,'sha256',p.sha256),
  'inputs',t.inputs,'outputs',z.outputs,'metrics',z.metrics,
  'review_required',z.review_required)
 INTO result
 FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id
  AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.artifacts p ON p.tenant_id=t.tenant_id AND p.id=t.params_artifact_id
 WHERE t.tenant_id=(source->>'tenant_id')::uuid
  AND t.id=(source->>'task_id')::uuid AND t.run_id=(source->>'run_id')::uuid
  AND t.state='succeeded' AND z.attempt=t.attempt
  AND z.fencing_token=t.fencing_token
  AND p.state='ready' AND p.verified_at IS NOT NULL
  AND p.object_version NOT IN('','null') AND p.media_type='application/json'
  AND p.bytes BETWEEN 2 AND 8388608
  AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp())
  AND octet_length(t.inputs::text)<=16777216
  AND octet_length(z.outputs::text)<=16777216
  AND octet_length(z.metrics::text)<=8388608
 FOR SHARE OF t,z,p;
 IF NOT FOUND OR result->'outputs' @> jsonb_build_array(jsonb_build_object(
  'artifact_id',source->>'artifact_id','sha256',source->>'sha256')) IS DISTINCT FROM true
  THEN RETURN NULL;END IF;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_final_task_evidence(uuid,uuid,integer,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_final_task_evidence(uuid,uuid,integer,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_final_task_evidence(uuid,uuid,integer,uuid)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
