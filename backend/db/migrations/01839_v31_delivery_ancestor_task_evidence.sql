-- +goose Up
-- Session-free equivalent of the canonical signing task ancestry query,
-- rooted only in a currently delegated Delivery export.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_ancestor_task_evidence(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,p_task uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;result jsonb;
BEGIN
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL OR p_task IS NULL THEN RETURN NULL;END IF;
 WITH RECURSIVE ancestors(id) AS (
  SELECT t.id FROM photo.tasks t WHERE t.tenant_id=(source->>'tenant_id')::uuid
   AND t.id=(source->>'task_id')::uuid AND t.run_id=(source->>'run_id')::uuid
  UNION
  SELECT next_task.id FROM ancestors ancestry
  JOIN photo.tasks child ON child.tenant_id=(source->>'tenant_id')::uuid
   AND child.id=ancestry.id AND child.state='succeeded'
  CROSS JOIN LATERAL (
   SELECT edge.prerequisite_id AS id FROM photo.task_edges edge
   JOIN photo.tasks parent ON parent.tenant_id=edge.tenant_id
    AND parent.id=edge.prerequisite_id AND parent.run_id=child.run_id
   WHERE edge.tenant_id=child.tenant_id AND edge.run_id=child.run_id
    AND edge.dependent_id=child.id
   UNION
   SELECT producer.id FROM photo.artifacts input_artifact
   JOIN photo.asset_versions version ON version.tenant_id=input_artifact.tenant_id
    AND version.master_artifact_id=input_artifact.id
   JOIN photo.assets asset ON asset.tenant_id=version.tenant_id
    AND asset.id=version.asset_id
   JOIN photo.tasks producer ON producer.tenant_id=input_artifact.tenant_id
    AND producer.id=input_artifact.created_by_task_id
   JOIN photo.task_results proof ON proof.tenant_id=producer.tenant_id
    AND proof.task_id=producer.id AND proof.id=producer.canonical_result_id
   WHERE input_artifact.tenant_id=child.tenant_id
    AND asset.project_id=(source->>'project_id')::uuid AND asset.deleted_at IS NULL
    AND version.created_by_run_id=producer.run_id AND producer.run_id<>child.run_id
    AND producer.state='succeeded' AND proof.attempt=producer.attempt
    AND proof.fencing_token=producer.fencing_token
    AND input_artifact.state='ready' AND input_artifact.verified_at IS NOT NULL
    AND input_artifact.object_version NOT IN('','null')
    AND input_artifact.media_type='application/json'
    AND input_artifact.bytes BETWEEN 2 AND 1048576
    AND (input_artifact.expires_at IS NULL OR input_artifact.expires_at>clock_timestamp())
    AND input_artifact.produced_attempt=producer.attempt
    AND input_artifact.produced_fence=producer.fencing_token
    AND (child.inputs @> jsonb_build_array(jsonb_build_object(
      'artifact_id',input_artifact.id::text,'sha256',input_artifact.sha256))
     OR (child.operation IN('render.compose','optical.negative')
      AND child.operation_version='1' AND EXISTS(
       SELECT 1 FROM photo.v2_task_inputs registered
       JOIN photo.v2_workflows consuming ON consuming.tenant_id=registered.tenant_id
        AND consuming.project_id=registered.project_id AND consuming.run_id=child.run_id
       WHERE registered.tenant_id=child.tenant_id AND registered.task_id=child.id
        AND registered.project_id=(source->>'project_id')::uuid
        AND registered.artifact_id=input_artifact.id
        AND registered.sha256=input_artifact.sha256
        AND registered.contract='WorkingImage')))
    AND (proof.outputs @> jsonb_build_array(jsonb_build_object(
      'artifact_id',input_artifact.id::text,'sha256',input_artifact.sha256))
     OR (producer.operation IN('render.compose','optical.negative')
      AND producer.operation_version='1' AND NOT producer.cancel_requested
      AND version.review_state='accepted'
      AND ((producer.operation='render.compose' AND input_artifact.output_role='document')
       OR (producer.operation='optical.negative' AND input_artifact.output_role='working_image'))
      AND EXISTS(
       SELECT 1 FROM photo.v2_workflows render_workflow
       JOIN photo.v2_artifact_links child_link ON child_link.tenant_id=render_workflow.tenant_id
        AND child_link.project_id=render_workflow.project_id
        AND child_link.workflow_id=render_workflow.id
       JOIN photo.v2_artifact_links root_link ON root_link.tenant_id=render_workflow.tenant_id
        AND root_link.project_id=render_workflow.project_id
        AND root_link.workflow_id=render_workflow.id
       JOIN photo.artifacts root_artifact ON root_artifact.tenant_id=root_link.tenant_id
        AND root_artifact.id=root_link.artifact_id
        AND root_artifact.sha256=root_link.sha256
       WHERE render_workflow.tenant_id=producer.tenant_id
        AND render_workflow.run_id=producer.run_id
        AND render_workflow.project_id=(source->>'project_id')::uuid
        AND render_workflow.renderer_version='v2.2'
        AND child_link.artifact_id=input_artifact.id
        AND child_link.sha256=input_artifact.sha256
        AND child_link.purpose=encode(sha256(convert_to(
         'tasks/'||producer.id||'/$/working_image','UTF8')),'hex')
        AND root_link.ordinal=0
        AND root_link.purpose=encode(sha256(convert_to(
         'tasks/'||producer.id||'/$','UTF8')),'hex')
        AND proof.outputs=jsonb_build_array(jsonb_build_object(
         'artifact_id',root_artifact.id::text,'sha256',root_artifact.sha256))
        AND root_artifact.created_by_task_id=producer.id
        AND root_artifact.produced_attempt=producer.attempt
        AND root_artifact.produced_fence=producer.fencing_token
        AND root_artifact.output_role='document'
        AND root_artifact.media_type='application/json'
        AND root_artifact.bytes BETWEEN 2 AND 1048576
        AND root_artifact.state='ready' AND root_artifact.verified_at IS NOT NULL
        AND root_artifact.object_version NOT IN('','null')
        AND (root_artifact.expires_at IS NULL OR root_artifact.expires_at>clock_timestamp()))))
  ) next_task
 ), bounded AS (SELECT id FROM ancestors LIMIT 5001)
 SELECT jsonb_build_object('task',t.id,'run',t.run_id,'result',z.id,
  'attempt',t.attempt,'fence',t.fencing_token,'operation',t.operation,
  'version',t.operation_version,'input_digest',t.input_digest,
  'result_digest',z.result_digest,
  'params',jsonb_build_object('artifact_id',p.id,'sha256',p.sha256),
  'inputs',t.inputs,'outputs',z.outputs,'metrics',z.metrics,
  'review_required',z.review_required)
 INTO result FROM photo.tasks t JOIN bounded a ON a.id=t.id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id
  AND z.id=t.canonical_result_id
 JOIN photo.artifacts p ON p.tenant_id=t.tenant_id AND p.id=t.params_artifact_id
 WHERE t.tenant_id=(source->>'tenant_id')::uuid AND t.id=p_task
  AND (SELECT count(*) FROM bounded)<=5000
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
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_ancestor_task_evidence(uuid,uuid,integer,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_ancestor_task_evidence(uuid,uuid,integer,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_ancestor_task_evidence(uuid,uuid,integer,uuid,uuid)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
