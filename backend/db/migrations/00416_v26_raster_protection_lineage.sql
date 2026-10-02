-- +goose Up
-- Preserve user publication scope and add existing multi-output rasterizer lineage.
-- V2 single-root producers require exact child-link proof in a separate branch.
ALTER FUNCTION photo.v26_render_protection_scope(uuid,uuid,uuid,uuid,jsonb,jsonb,uuid)
 RENAME TO v26_render_user_protection_scope;
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_raster_protection_scope(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_body jsonb,p_node jsonb,p_task uuid) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ref jsonb; raw text; mask jsonb; source jsonb; binding jsonb;
BEGIN
 ref:=p_body->'recipe'->'protected_mask';raw:=p_node->>'protection_jcs';binding:=p_body->'scalars'->'source';
 IF ref IS NULL OR ref='null'::jsonb THEN RETURN raw IS NULL OR raw='';END IF;
 IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR ref-'artifact_id'-'sha256'<>'{}'::jsonb
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR raw IS NULL OR octet_length(raw) NOT BETWEEN 2 AND 1048576
  OR encode(sha256(convert_to(raw,'UTF8')),'hex') IS DISTINCT FROM ref->>'sha256' THEN RETURN false;END IF;
 IF p_node->>'source_jcs' IS NULL OR octet_length(p_node->>'source_jcs') NOT BETWEEN 2 AND 1048576
  OR encode(sha256(convert_to(p_node->>'source_jcs','UTF8')),'hex') IS DISTINCT FROM binding->'source'->>'sha256' THEN RETURN false;END IF;
 mask:=raw::jsonb;source:=(p_node->>'source_jcs')::jsonb;
 IF jsonb_typeof(mask) IS DISTINCT FROM 'object'
  OR NOT mask ?& ARRAY['artifact','canvas','transform_chain','object_id','selector','confidence','model_lock_id','protected']
  OR mask-'artifact'-'canvas'-'transform_chain'-'object_id'-'selector'-'confidence'-'model_lock_id'-'protected'<>'{}'::jsonb
  OR mask->'protected' IS DISTINCT FROM 'true'::jsonb
  OR jsonb_typeof(mask->'selector') IS DISTINCT FROM 'string' OR length(mask->>'selector') NOT BETWEEN 1 AND 120
  OR jsonb_typeof(mask->'confidence') IS DISTINCT FROM 'number'
  OR (mask->>'confidence')::numeric NOT BETWEEN 0 AND 1
  OR mask->'canvas' IS DISTINCT FROM source->'canvas'
  OR mask->'transform_chain' IS DISTINCT FROM source->'transform_chain'
  OR jsonb_typeof(mask->'artifact') IS DISTINCT FROM 'object'
  OR (mask->'artifact')-'artifact_id'-'sha256'<>'{}'::jsonb THEN RETURN false;END IF;

 IF NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
  WHERE v.tenant_id=p_tenant AND v.id=(binding->>'asset_version_id')::uuid AND a.project_id=p_project
   AND a.deleted_at IS NULL AND a.upload_state='verified' AND v.review_state='accepted'
   AND v.master_artifact_id=(binding->'source'->>'artifact_id')::uuid AND v.content_hash=binding->'source'->>'sha256'
   AND NOT EXISTS(SELECT 1 FROM photo.asset_versions newer WHERE newer.tenant_id=v.tenant_id AND newer.asset_id=v.asset_id AND newer.version_no>v.version_no)) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts root
  JOIN photo.tasks producer ON producer.tenant_id=root.tenant_id AND producer.id=root.created_by_task_id
  JOIN photo.runs w ON w.tenant_id=producer.tenant_id AND w.id=producer.run_id
   AND w.kind<>'v2' AND w.config_snapshot->>'project_id'=p_project::text
  JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.task_id=producer.id AND result.id=producer.canonical_result_id
  JOIN photo.artifacts pixels ON pixels.tenant_id=root.tenant_id AND pixels.id=(mask->'artifact'->>'artifact_id')::uuid
   AND pixels.sha256=mask->'artifact'->>'sha256' AND pixels.created_by_task_id=producer.id
  WHERE root.tenant_id=p_tenant AND root.id=(ref->>'artifact_id')::uuid AND root.sha256=ref->>'sha256'
   AND root.bytes=octet_length(raw) AND root.media_type='application/json' AND root.output_role='mask_protected_ref'
   AND producer.inputs @> jsonb_build_array(binding->'source')
   AND producer.operation='mask.rasterize' AND producer.operation_version='1' AND producer.state='succeeded'
   AND NOT producer.cancel_requested AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   AND root.produced_attempt=result.attempt AND root.produced_fence=result.fencing_token
   AND pixels.produced_attempt=result.attempt AND pixels.produced_fence=result.fencing_token
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',root.id::text,'sha256',root.sha256),
    jsonb_build_object('artifact_id',pixels.id::text,'sha256',pixels.sha256))
   AND root.state='ready' AND root.verified_at IS NOT NULL AND root.object_version IS NOT NULL AND root.object_version NOT IN('','null')
   AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
   AND pixels.output_role='mask_protected' AND pixels.media_type='application/octet-stream' AND pixels.bytes>0
   AND pixels.state='ready' AND pixels.verified_at IS NOT NULL AND pixels.object_version IS NOT NULL AND pixels.object_version NOT IN('','null')
   AND (pixels.expires_at IS NULL OR pixels.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 IF p_task IS NOT NULL AND (NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.project_id=p_project AND i.task_id=p_task
  AND i.artifact_id=(ref->>'artifact_id')::uuid AND i.sha256=ref->>'sha256' AND i.contract='MaskRef')
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.project_id=p_project AND i.task_id=p_task
   AND i.artifact_id=(mask->'artifact'->>'artifact_id')::uuid AND i.sha256=mask->'artifact'->>'sha256' AND i.contract='NpyArray')) THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_raster_protection_scope(uuid,uuid,uuid,uuid,jsonb,jsonb,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_raster_protection_scope(uuid,uuid,uuid,uuid,jsonb,jsonb,uuid) FROM PUBLIC;
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_protection_scope(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_body jsonb,p_node jsonb,p_task uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v26_render_user_protection_scope(p_tenant,p_project,p_actor,p_session,p_body,p_node,p_task)
 OR photo.v26_render_raster_protection_scope(p_tenant,p_project,p_actor,p_session,p_body,p_node,p_task);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_protection_scope(uuid,uuid,uuid,uuid,jsonb,jsonb,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_protection_scope(uuid,uuid,uuid,uuid,jsonb,jsonb,uuid) FROM PUBLIC;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
