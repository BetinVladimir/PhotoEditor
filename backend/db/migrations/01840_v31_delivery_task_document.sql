-- +goose Up
-- Delegated equivalent of the task-scoped signing JSON-document lookup.
-- Caller still verifies the immutable object bytes and repeats this lookup.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_task_document(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_task uuid,p_artifact uuid,p_sha text,p_max integer,p_bucket text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;result jsonb;
BEGIN
 IF p_max<2 OR p_max>8388608 OR p_sha!~'^[0-9a-f]{64}$'
  OR length(p_bucket)<3 OR length(p_bucket)>63 THEN RETURN NULL;END IF;
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN NULL;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('bucket',a.bucket,'key',a.object_key,
  'version',a.object_version,'sha256',a.sha256,'bytes',a.bytes,
  'media_type',a.media_type)
 INTO result FROM photo.artifacts a
 WHERE a.tenant_id=(source->>'tenant_id')::uuid AND a.id=p_artifact
  AND a.sha256=p_sha AND a.bucket=p_bucket
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version NOT IN('','null')
  AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND p_max
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND ((a.id=(e->'params'->>'artifact_id')::uuid
    AND a.sha256=e->'params'->>'sha256')
   OR e->'inputs' @> jsonb_build_array(jsonb_build_object(
    'artifact_id',a.id::text,'sha256',a.sha256))
   OR (e->'outputs' @> jsonb_build_array(jsonb_build_object(
     'artifact_id',a.id::text,'sha256',a.sha256))
    AND a.created_by_task_id=p_task
    AND a.produced_attempt=(e->>'attempt')::integer
    AND a.produced_fence=(e->>'fence')::bigint)
   OR (e->>'operation' IN('render.compose','optical.negative')
    AND a.created_by_task_id=p_task
    AND a.produced_attempt=(e->>'attempt')::integer
    AND a.produced_fence=(e->>'fence')::bigint
    AND a.output_role IN('document','working_image','mask','report')
    AND jsonb_array_length(e->'outputs')=1
    AND EXISTS(SELECT 1 FROM photo.v2_workflows w
     JOIN photo.v2_artifact_links child ON child.tenant_id=w.tenant_id
      AND child.workflow_id=w.id AND child.project_id=w.project_id
     JOIN photo.v2_artifact_links root ON root.tenant_id=w.tenant_id
      AND root.workflow_id=w.id AND root.project_id=w.project_id
     WHERE w.tenant_id=a.tenant_id AND w.run_id=(e->>'run')::uuid
      AND w.project_id=(source->>'project_id')::uuid
      AND child.artifact_id=a.id AND child.sha256=a.sha256
      AND (e->>'operation'<>'optical.negative' OR
       child.purpose<>encode(sha256(convert_to(
        'tasks/'||p_task||'/$/generated_mask','UTF8')),'hex'))
      AND child.purpose IN(
       encode(sha256(convert_to('tasks/'||p_task||'/$/working_image','UTF8')),'hex'),
       encode(sha256(convert_to('tasks/'||p_task||'/$/report','UTF8')),'hex'),
       encode(sha256(convert_to('tasks/'||p_task||'/$/changed_mask','UTF8')),'hex'),
       encode(sha256(convert_to('tasks/'||p_task||'/$/generated_mask','UTF8')),'hex'))
      AND root.ordinal=0
      AND root.purpose=encode(sha256(convert_to('tasks/'||p_task||'/$','UTF8')),'hex')
      AND root.artifact_id=(e->'outputs'->0->>'artifact_id')::uuid
      AND root.sha256=e->'outputs'->0->>'sha256'))
   OR (e->>'operation' IN('render.compose','optical.negative')
    AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i
     JOIN photo.v2_workflows w ON w.tenant_id=i.tenant_id
      AND w.run_id=(e->>'run')::uuid AND w.project_id=i.project_id
     WHERE i.tenant_id=a.tenant_id AND i.task_id=p_task
      AND i.project_id=(source->>'project_id')::uuid
      AND i.artifact_id=a.id AND i.sha256=a.sha256
      AND i.contract='InputClosureChunk')))
 FOR SHARE OF a;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_task_document(uuid,uuid,integer,uuid,uuid,uuid,text,integer,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_task_document(uuid,uuid,integer,uuid,uuid,uuid,text,integer,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_task_document(uuid,uuid,integer,uuid,uuid,uuid,text,integer,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
