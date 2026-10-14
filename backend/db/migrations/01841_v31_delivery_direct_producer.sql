-- +goose Up
-- Direct canonical output producer under the exact delegated ancestry.
-- Adopted render/negative child WorkingImages require separate authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_direct_producer(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_artifact uuid,p_sha text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;candidate jsonb;e jsonb;
BEGIN
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL OR p_artifact IS NULL OR p_sha!~'^[0-9a-f]{64}$'
  THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('task',a.created_by_task_id,
  'attempt',a.produced_attempt,'fence',a.produced_fence)
 INTO candidate FROM photo.artifacts a
 JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id
  AND z.id=t.canonical_result_id
 WHERE a.tenant_id=(source->>'tenant_id')::uuid AND a.id=p_artifact
  AND a.sha256=p_sha AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version NOT IN('','null')
  AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND t.state='succeeded' AND a.produced_attempt=t.attempt
  AND a.produced_fence=t.fencing_token AND z.attempt=t.attempt
  AND z.fencing_token=t.fencing_token
  AND z.outputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',a.id::text,'sha256',a.sha256))
 FOR SHARE OF a,t,z;
 IF candidate IS NULL THEN RETURN NULL;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,
  p_preset,(candidate->>'task')::uuid);
 IF e IS NULL OR (e->>'attempt')::integer<>(candidate->>'attempt')::integer
  OR (e->>'fence')::bigint<>(candidate->>'fence')::bigint
  OR NOT e->'outputs' @> jsonb_build_array(jsonb_build_object(
   'artifact_id',p_artifact::text,'sha256',p_sha)) THEN RETURN NULL;END IF;
 RETURN candidate;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_direct_producer(uuid,uuid,integer,uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_direct_producer(uuid,uuid,integer,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_direct_producer(uuid,uuid,integer,uuid,uuid,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
