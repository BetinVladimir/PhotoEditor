-- +goose Up
-- Delegated candidate review metadata for an exact adopted render/negative
-- child. Go must verify append-once decision hash/selection and root bytes.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_candidate_review(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_task uuid,p_candidate uuid,p_candidate_sha text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;producer jsonb;e jsonb;found_count integer;result jsonb;
BEGIN
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 producer:=photo.v31_delivery_adopted_image_producer(p_expansion,p_token,
  p_ordinal,p_preset,p_candidate,p_candidate_sha);
 IF source IS NULL OR producer IS NULL OR
  (producer->>'task')::uuid<>p_task THEN RETURN NULL;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,
  p_preset,p_task);
 IF e IS NULL OR e->>'operation' NOT IN('render.compose','optical.negative')
  THEN RETURN NULL;END IF;
 SELECT count(*) INTO found_count FROM (
  SELECT 1 FROM photo.reviews v
  WHERE v.tenant_id=(source->>'tenant_id')::uuid
   AND v.run_id=(e->>'run')::uuid AND v.task_id=p_task
  ORDER BY v.id LIMIT 2) observed;
 IF found_count=0 THEN RETURN jsonb_build_object('count',0);END IF;
 IF found_count<>1 THEN RETURN jsonb_build_object('count',2);END IF;
 SELECT jsonb_build_object('count',1,'id',v.id,'revision',v.revision,
  'state',v.state,'kind',v.kind,'decision',coalesce(v.decision,''),
  'route',coalesce(v.decision_route,''),
  'decision_hash',coalesce(v.decision_hash,''),
  'actor',v.decided_by,'dated',v.decided_at IS NOT NULL,
  'selection',v.selection,'candidate_artifact_id',v.candidate_artifact_id,
  'candidate_sha',v.candidate_hash,'base_artifact_id',v.base_artifact_id,
  'base_sha',b.sha256)
 INTO result FROM photo.reviews v
 LEFT JOIN photo.artifacts b ON b.tenant_id=v.tenant_id
  AND b.id=v.base_artifact_id
 WHERE v.tenant_id=(source->>'tenant_id')::uuid
  AND v.run_id=(e->>'run')::uuid AND v.task_id=p_task
 ORDER BY v.id LIMIT 1 FOR SHARE OF v;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_candidate_review(uuid,uuid,integer,uuid,uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_candidate_review(uuid,uuid,integer,uuid,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_candidate_review(uuid,uuid,integer,uuid,uuid,uuid,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
