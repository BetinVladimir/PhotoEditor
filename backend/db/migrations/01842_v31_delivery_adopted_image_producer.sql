-- +goose Up
-- Scoped render/negative adopted WorkingImage child producer. Root bytes and
-- ImageResult child equality remain duties of the Go object verifier.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_adopted_image_producer(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_artifact uuid,p_sha text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;candidate jsonb;e jsonb;
BEGIN
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL OR p_artifact IS NULL OR p_sha!~'^[0-9a-f]{64}$'
  THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('task',t.id,'attempt',t.attempt,
  'fence',t.fencing_token,'operation',t.operation,
  'root_artifact',root_artifact.id,'root_sha',root_artifact.sha256)
 INTO candidate FROM photo.artifacts a
 JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id
  AND z.id=t.canonical_result_id
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 JOIN photo.v2_artifact_links child ON child.tenant_id=w.tenant_id
  AND child.project_id=w.project_id AND child.workflow_id=w.id
 JOIN photo.v2_artifact_links root ON root.tenant_id=w.tenant_id
  AND root.project_id=w.project_id AND root.workflow_id=w.id
 JOIN photo.artifacts root_artifact ON root_artifact.tenant_id=root.tenant_id
  AND root_artifact.id=root.artifact_id AND root_artifact.sha256=root.sha256
 WHERE a.tenant_id=(source->>'tenant_id')::uuid AND a.id=p_artifact
  AND a.sha256=p_sha AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version NOT IN('','null')
  AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND ((t.operation='render.compose' AND a.output_role='document')
   OR (t.operation='optical.negative' AND a.output_role='working_image'))
  AND t.operation_version='1' AND t.state='succeeded'
  AND NOT t.cancel_requested AND a.produced_attempt=t.attempt
  AND a.produced_fence=t.fencing_token
  AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  AND w.project_id=(source->>'project_id')::uuid
  AND child.artifact_id=a.id AND child.sha256=a.sha256
  AND child.purpose=encode(sha256(convert_to(
   'tasks/'||t.id||'/$/working_image','UTF8')),'hex')
  AND root.ordinal=0
  AND root.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex')
  AND z.outputs=jsonb_build_array(jsonb_build_object(
   'artifact_id',root_artifact.id::text,'sha256',root_artifact.sha256))
  AND root_artifact.created_by_task_id=t.id
  AND root_artifact.produced_attempt=t.attempt
  AND root_artifact.produced_fence=t.fencing_token
  AND root_artifact.output_role='document'
  AND root_artifact.media_type='application/json'
  AND root_artifact.bytes BETWEEN 2 AND 1048576
  AND root_artifact.state='ready' AND root_artifact.verified_at IS NOT NULL
  AND root_artifact.object_version NOT IN('','null')
  AND (root_artifact.expires_at IS NULL OR root_artifact.expires_at>clock_timestamp())
 FOR SHARE OF a,t,z,w,child,root,root_artifact;
 IF candidate IS NULL THEN RETURN NULL;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,
  p_preset,(candidate->>'task')::uuid);
 IF e IS NULL OR e->>'operation'<>candidate->>'operation'
  OR (e->>'attempt')::integer<>(candidate->>'attempt')::integer
  OR (e->>'fence')::bigint<>(candidate->>'fence')::bigint
  OR e->'outputs'<>jsonb_build_array(jsonb_build_object(
   'artifact_id',candidate->>'root_artifact','sha256',candidate->>'root_sha'))
  THEN RETURN NULL;END IF;
 RETURN candidate;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_adopted_image_producer(uuid,uuid,integer,uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_adopted_image_producer(uuid,uuid,integer,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_adopted_image_producer(uuid,uuid,integer,uuid,uuid,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
