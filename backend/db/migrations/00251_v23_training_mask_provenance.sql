-- V23 training mask root must belong to the correction's immutable document
-- closure; its array must be a canonical task output of the same project.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_mask_provenance(
 p_tenant uuid,p_project uuid,p_event uuid,p_root uuid,p_root_sha text,
 p_child uuid,p_child_sha text,p_protected boolean,
 p_actor uuid,p_auth_session uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_event IS NULL OR
  p_root IS NULL OR p_child IS NULL OR p_protected IS NULL OR
  p_actor IS NULL OR p_auth_session IS NULL OR
  coalesce(p_root_sha,'') !~ '^[0-9a-f]{64}$' OR
  coalesce(p_child_sha,'') !~ '^[0-9a-f]{64}$' OR
  NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_auth_session)
  THEN RETURN false;
 END IF;
 RETURN EXISTS(
  SELECT 1 FROM photo.correction_events e
  JOIN photo.v2_document_refs link ON link.tenant_id=e.tenant_id
   AND link.project_id=e.project_id
   AND link.document_id IN(e.base_document_id,e.approved_document_id)
   AND link.artifact_id=p_root AND link.sha256=p_root_sha
  JOIN photo.artifacts root ON root.tenant_id=link.tenant_id
   AND root.id=link.artifact_id AND root.sha256=link.sha256
  JOIN photo.artifacts child ON child.tenant_id=e.tenant_id
   AND child.id=p_child AND child.sha256=p_child_sha
  JOIN photo.tasks producer ON producer.tenant_id=child.tenant_id
   AND producer.id=child.created_by_task_id
  JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
   AND result.task_id=producer.id AND result.id=producer.canonical_result_id
   AND result.attempt=producer.attempt
   AND result.fencing_token=producer.fencing_token
  JOIN photo.runs run_row ON run_row.tenant_id=producer.tenant_id
   AND run_row.id=producer.run_id
  LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run_row.tenant_id
   AND workflow.run_id=run_row.id
  JOIN photo.consent_grants consent ON consent.tenant_id=e.tenant_id
   AND consent.project_id=e.project_id AND consent.id=e.consent_id
  WHERE e.tenant_id=p_tenant AND e.project_id=p_project AND e.id=p_event
   AND e.learn AND consent.status='granted' AND consent.epoch=e.consent_epoch
   AND consent.person_id IS NOT DISTINCT FROM e.person_id
   AND consent.purpose=CASE WHEN e.person_id IS NULL THEN
    'learning_photographer' ELSE 'learning_person' END
   AND (e.person_id IS NULL OR EXISTS(SELECT 1 FROM photo.project_persons person
    WHERE person.tenant_id=e.tenant_id AND person.project_id=e.project_id
     AND person.id=e.person_id AND person.state='active'))
   AND root.state='ready' AND root.media_type='application/json'
   AND root.verified_at IS NOT NULL AND root.object_version IS NOT NULL
   AND root.object_version NOT IN ('','null')
   AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
   AND child.state='ready' AND child.verified_at IS NOT NULL
   AND child.object_version IS NOT NULL
   AND child.object_version NOT IN ('','null')
   AND (child.expires_at IS NULL OR child.expires_at>clock_timestamp())
   AND child.media_type='application/octet-stream' AND child.bytes>=32
   AND child.output_role=ANY(CASE WHEN p_protected THEN
    ARRAY['mask_protected','defect_protection','transformed_mask']::text[]
    ELSE ARRAY['person_mask','mask_target','transformed_mask']::text[] END)
   AND child.produced_attempt=producer.attempt
   AND child.produced_fence=producer.fencing_token
   AND producer.state='succeeded' AND producer.canonical_result_id IS NOT NULL
   AND (workflow.project_id=p_project OR
    run_row.config_snapshot->>'project_id'=p_project::text)
   AND jsonb_typeof(result.outputs)='array'
   AND result.outputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',child.id,'sha256',child.sha256)));
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_mask_provenance(
 uuid,uuid,uuid,uuid,text,uuid,text,boolean,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_mask_provenance(
 uuid,uuid,uuid,uuid,text,uuid,text,boolean,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_mask_provenance(
 uuid,uuid,uuid,uuid,text,uuid,text,boolean,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
