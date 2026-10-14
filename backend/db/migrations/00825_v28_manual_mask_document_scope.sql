-- +goose Up
-- Relational original-manual path only. Caller must read and validate exact
-- RasterizedMask/Params/vectors/MaskRef/pixel bytes; this is not lease admission.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_manual_mask_document_scope(p_tenant uuid,p_project uuid,
 p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb,p_doc uuid,p_doc_sha text,
 p_pixel uuid,p_pixel_sha text,p_protected boolean) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE matches bigint;
BEGIN
 IF p_protected IS NULL OR p_doc IS NULL OR p_pixel IS NULL
  OR p_doc_sha IS NULL OR p_doc_sha !~ '^[0-9a-f]{64}$'
  OR p_pixel_sha IS NULL OR p_pixel_sha !~ '^[0-9a-f]{64}$'
  OR photo.v28_local_portrait_source_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base)
   IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT count(*) INTO matches FROM photo.artifacts doc
 JOIN photo.artifacts pixel ON pixel.tenant_id=doc.tenant_id AND pixel.id=p_pixel AND pixel.sha256=p_pixel_sha
 JOIN photo.tasks t ON t.tenant_id=doc.tenant_id AND t.id=doc.created_by_task_id AND t.id=pixel.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=t.tenant_id AND result.id=t.canonical_result_id
  AND result.task_id=t.id AND result.attempt=t.attempt AND result.fencing_token=t.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=t.tenant_id AND attempt.task_id=t.id
  AND attempt.attempt=t.attempt AND attempt.fencing_token=t.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.interactive_tasks i ON i.tenant_id=t.tenant_id AND i.task_id=t.id
 JOIN photo.artifacts root ON root.tenant_id=t.tenant_id AND root.created_by_task_id=t.id
  AND root.output_role='rasterized_mask'
 WHERE doc.tenant_id=p_tenant AND doc.id=p_doc AND doc.sha256=p_doc_sha
  AND doc.output_role=CASE WHEN p_protected THEN 'mask_guard_ref' ELSE 'mask_target_ref' END
  AND pixel.output_role=CASE WHEN p_protected THEN 'mask_protected' ELSE 'mask_target' END
  AND t.operation='mask.rasterize' AND t.operation_version='1' AND t.state='succeeded'
  AND NOT t.cancel_requested AND r.kind='mask' AND r.graph_sealed AND NOT r.cancel_requested
  AND i.source_artifact_id=(p_base->'source'->>'artifact_id')::uuid
  AND r.config_snapshot->>'asset_version_id'=p_base->>'asset_version_id'
  AND r.config_snapshot->>'source_sha256'=p_base->'source'->>'sha256'
  AND doc.media_type='application/json' AND doc.bytes BETWEEN 2 AND 1048576
  AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 1048576
  AND pixel.media_type='application/octet-stream' AND pixel.bytes BETWEEN 32 AND 800065548
  AND doc.produced_attempt=t.attempt AND doc.produced_fence=t.fencing_token
  AND root.produced_attempt=t.attempt AND root.produced_fence=t.fencing_token
  AND pixel.produced_attempt=t.attempt AND pixel.produced_fence=t.fencing_token
  AND jsonb_typeof(result.outputs)='array'
  AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',doc.id,'sha256',doc.sha256),
   jsonb_build_object('artifact_id',pixel.id,'sha256',pixel.sha256),
   jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
  AND NOT EXISTS(SELECT 1 FROM (VALUES(doc.state,doc.verified_at,doc.object_version,doc.expires_at),
   (pixel.state,pixel.verified_at,pixel.object_version,pixel.expires_at),
   (root.state,root.verified_at,root.object_version,root.expires_at)) objects(state,verified,version,expires)
   WHERE state IS DISTINCT FROM 'ready' OR verified IS NULL OR version IS NULL
    OR version IN('','null') OR expires<=clock_timestamp());
 RETURN matches=1;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_manual_mask_document_scope(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_manual_mask_document_scope(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_manual_mask_document_scope(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,boolean) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
