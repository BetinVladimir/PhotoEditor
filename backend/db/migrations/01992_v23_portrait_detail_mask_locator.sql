-- Authorize one binary mask only when the exact event-linked portrait.detail
-- Params embeds it and the canonical task registered it as an input.
-- This locator does not enable Evaluate or register a training closure.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_portrait_detail_mask_artifact(
 p_tenant uuid,p_project uuid,p_actor uuid,p_auth_session uuid,
 p_event uuid,p_mask uuid,p_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,
 sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source_row record;a photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL OR p_event IS NULL OR p_mask IS NULL OR
  p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 SELECT * INTO source_row FROM photo.v23_portrait_detail_event_link_source(
  p_tenant,p_project,p_actor,p_auth_session,p_event);
 IF NOT FOUND THEN RETURN;END IF;
 IF NOT EXISTS(
  SELECT 1 FROM photo.apply_nodes node
  JOIN photo.tasks task ON task.tenant_id=node.tenant_id AND
   task.id=node.id AND task.run_id=node.run_id AND
   task.operation='portrait.detail' AND task.operation_version='1' AND
   task.state='succeeded' AND task.canonical_result_id=source_row.result_id
  WHERE node.tenant_id=p_tenant AND node.run_id=source_row.run_id AND
   node.id=source_row.task_id AND node.phase='portrait_detail' AND
   node.operation='portrait.detail' AND
   node.params_sha256=source_row.params_sha AND
   task.params_artifact_id=source_row.params_id AND
   task.inputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',p_mask::text,'sha256',p_sha)) AND
   (
    EXISTS(SELECT 1 FROM jsonb_array_elements(
     CASE WHEN jsonb_typeof(node.params->'regions')='array'
      THEN node.params->'regions' ELSE '[]'::jsonb END) region
     WHERE region.value->'mask'->'artifact'=jsonb_build_object(
      'artifact_id',p_mask,'sha256',p_sha) AND
      region.value->'mask'->>'protected'='false')
    OR EXISTS(SELECT 1 FROM jsonb_array_elements(
     CASE WHEN jsonb_typeof(node.params->'protected')='array'
      THEN node.params->'protected' ELSE '[]'::jsonb END) guard
     WHERE guard.value->'artifact'=jsonb_build_object(
      'artifact_id',p_mask,'sha256',p_sha) AND
      guard.value->>'protected'='true')
   )
 ) THEN RETURN;END IF;
 SELECT * INTO a FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND
  x.id=p_mask AND x.sha256=p_sha AND x.state='ready' AND
  x.media_type IN ('application/octet-stream','application/x-npy') AND
  x.verified_at IS NOT NULL AND x.object_version IS NOT NULL AND
  x.object_version NOT IN ('','null') AND
  x.bytes BETWEEN 32 AND 800065548 AND
  (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,a.sha256,
  a.bytes,a.media_type;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_portrait_detail_mask_artifact(
 uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_portrait_detail_mask_artifact(
 uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_portrait_detail_mask_artifact(
 uuid,uuid,uuid,uuid,uuid,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
