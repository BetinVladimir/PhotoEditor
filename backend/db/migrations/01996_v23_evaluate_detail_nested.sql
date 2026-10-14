-- Fenced version locator for inputs named by the exact linked Apply producer.
-- The evidence JSON alone never grants nested object authority.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_evaluate_detail_nested(
 p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,p_event uuid,
 p_evidence uuid,p_evidence_sha text,p_role text,p_artifact uuid,p_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,
 media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE origin record; a photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR
  p_token IS NULL OR p_event IS NULL OR p_evidence IS NULL OR
  p_artifact IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR
  p_role NOT IN ('image','params','resolved','mask') THEN RETURN; END IF;
 SELECT * INTO origin FROM photo.v23_evaluate_detail_authority(
  p_tenant,p_run,p_expansion,p_token,p_event,p_evidence,p_evidence_sha);
 IF NOT FOUND THEN RETURN; END IF;
 IF p_role='image' AND (origin.image_id IS DISTINCT FROM p_artifact OR
    origin.image_sha IS DISTINCT FROM p_sha) THEN RETURN; END IF;
 IF p_role='params' AND (origin.params_id IS DISTINCT FROM p_artifact OR
    origin.params_sha IS DISTINCT FROM p_sha) THEN RETURN; END IF;
 IF p_role='resolved' AND (origin.resolved_id IS DISTINCT FROM p_artifact OR
    origin.resolved_sha IS DISTINCT FROM p_sha) THEN RETURN; END IF;
 IF p_role='mask' AND NOT EXISTS(
  SELECT 1 FROM photo.apply_nodes node
  JOIN photo.tasks task ON task.tenant_id=node.tenant_id AND
   task.id=node.id AND task.run_id=node.run_id AND
   task.operation='portrait.detail' AND task.operation_version='1' AND
   task.state='succeeded' AND task.canonical_result_id=origin.producer_result_id
  WHERE node.tenant_id=p_tenant AND node.run_id=origin.producer_run_id AND
   node.id=origin.producer_task_id AND node.phase='portrait_detail' AND
   node.operation='portrait.detail' AND
   node.params_sha256=origin.params_sha AND
   task.params_artifact_id=origin.params_id AND
   task.inputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',p_artifact::text,'sha256',p_sha)) AND
   (EXISTS(SELECT 1 FROM jsonb_array_elements(
      CASE WHEN jsonb_typeof(node.params->'regions')='array'
       THEN node.params->'regions' ELSE '[]'::jsonb END) region
     WHERE region.value->'mask'->'artifact'=jsonb_build_object(
      'artifact_id',p_artifact,'sha256',p_sha) AND
      region.value->'mask'->>'protected'='false') OR
    EXISTS(SELECT 1 FROM jsonb_array_elements(
      CASE WHEN jsonb_typeof(node.params->'protected')='array'
       THEN node.params->'protected' ELSE '[]'::jsonb END) guard
     WHERE guard.value->'artifact'=jsonb_build_object(
      'artifact_id',p_artifact,'sha256',p_sha) AND
      guard.value->>'protected'='true'))
 ) THEN RETURN; END IF;
 SELECT * INTO a FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND
  x.id=p_artifact AND x.sha256=p_sha AND x.state='ready' AND
  x.verified_at IS NOT NULL AND x.object_version IS NOT NULL AND
  x.object_version NOT IN ('','null') AND
  (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) AND
  ((p_role='mask' AND x.media_type IN
    ('application/octet-stream','application/x-npy') AND
    x.bytes BETWEEN 32 AND 800065548) OR
   (p_role<>'mask' AND x.media_type='application/json' AND
    x.bytes BETWEEN 2 AND 1048576)) FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_detail_nested(
 uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_detail_nested(
 uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_detail_nested(
 uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
