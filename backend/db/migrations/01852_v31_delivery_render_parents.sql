-- +goose Up
-- Ordered adopted ImageResult parents registered for one delegated render task.
-- Null means any parent failed current physical/task/link authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_render_parents(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,p_task uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;total integer;accepted integer;result jsonb;
BEGIN
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN NULL;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL OR e->>'operation'<>'render.compose' OR e->>'version'<>'1'
  THEN RETURN NULL;END IF;
 WITH parents AS (
  SELECT i.ordinal,i.artifact_id,i.sha256,a.created_by_task_id AS producer,
   coalesce(a.state='ready' AND a.verified_at IS NOT NULL
    AND a.object_version NOT IN('','null') AND a.media_type='application/json'
    AND a.output_role='document' AND a.bytes BETWEEN 2 AND 1048576
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
    AND p.state='succeeded' AND NOT p.cancel_requested AND p.id<>p_task
    AND a.produced_attempt=p.attempt AND a.produced_fence=p.fencing_token
    AND z.attempt=p.attempt AND z.fencing_token=p.fencing_token
    AND z.outputs=jsonb_build_array(jsonb_build_object(
     'artifact_id',a.id::text,'sha256',a.sha256))
    AND EXISTS(SELECT 1 FROM photo.v2_workflows w
     JOIN photo.v2_artifact_links l ON l.tenant_id=w.tenant_id
      AND l.workflow_id=w.id AND l.project_id=w.project_id
     WHERE w.tenant_id=p.tenant_id AND w.run_id=p.run_id
      AND w.project_id=(source->>'project_id')::uuid
      AND w.workflow_type='preview' AND w.renderer_version='v2.2'
      AND l.artifact_id=a.id AND l.sha256=a.sha256 AND l.ordinal=0
      AND l.purpose=encode(sha256(convert_to(
       'tasks/'||p.id||'/$','UTF8')),'hex')),false) AS valid
  FROM photo.v2_task_inputs i
  LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id
   AND a.id=i.artifact_id AND a.sha256=i.sha256
  LEFT JOIN photo.tasks p ON p.tenant_id=a.tenant_id
   AND p.id=a.created_by_task_id
  LEFT JOIN photo.task_results z ON z.tenant_id=p.tenant_id
   AND z.task_id=p.id AND z.id=p.canonical_result_id
  WHERE i.tenant_id=(source->>'tenant_id')::uuid AND i.task_id=p_task
   AND i.project_id=(source->>'project_id')::uuid AND i.contract='ImageResult'
  ORDER BY i.ordinal,i.artifact_id LIMIT 34
 )
 SELECT count(*),count(*) FILTER(WHERE valid),
  coalesce(jsonb_agg(jsonb_build_object('artifact_id',artifact_id,
   'sha256',sha256,'producer',producer) ORDER BY ordinal,artifact_id),'[]'::jsonb)
 INTO total,accepted,result FROM parents;
 IF total>33 OR accepted<>total OR
  (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(result))<>total
  THEN RETURN NULL;END IF;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_render_parents(uuid,uuid,integer,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_render_parents(uuid,uuid,integer,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_render_parents(uuid,uuid,integer,uuid,uuid)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
