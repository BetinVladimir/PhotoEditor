-- V23 scoped EditingRecipeV3 source for training admission.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_document_source(
 p_tenant uuid,p_project uuid,p_event uuid,p_side text,
 p_actor uuid,p_auth_session uuid)
RETURNS TABLE(document_id uuid,artifact_id uuid,sha256 text,
 bytes bigint,bucket text,object_key text,object_version text,
 media_type text,stream_id uuid,version_no bigint,approval_id uuid)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_event IS NULL OR
  p_actor IS NULL OR p_auth_session IS NULL OR
  p_side IS NULL OR p_side NOT IN ('base','approved') OR
  NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_auth_session) THEN
  RAISE EXCEPTION 'V23_TRAIN_DOCUMENT_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 RETURN QUERY
 SELECT d.id,d.artifact_id,d.sha256,a.bytes,a.bucket,a.object_key,
  a.object_version,a.media_type,d.stream_id,d.version_no,approval.id
 FROM photo.correction_events e
 JOIN photo.v2_documents d ON d.tenant_id=e.tenant_id
  AND d.project_id=e.project_id
  AND d.id=CASE WHEN p_side='base' THEN e.base_document_id
                ELSE e.approved_document_id END
 JOIN photo.v2_document_streams stream ON stream.tenant_id=d.tenant_id
  AND stream.project_id=d.project_id AND stream.id=d.stream_id
  AND stream.stream_type='draft'
  AND stream.asset_version_id=e.asset_version_id
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id
  AND a.id=d.artifact_id AND a.sha256=d.sha256
 LEFT JOIN photo.v2_document_approvals approval
  ON approval.tenant_id=d.tenant_id AND approval.project_id=d.project_id
  AND approval.document_id=d.id AND approval.recipe_sha=d.sha256
 LEFT JOIN photo.artifacts resolved ON resolved.tenant_id=approval.tenant_id
  AND resolved.id=approval.resolved_artifact_id
 JOIN photo.consent_grants consent ON consent.tenant_id=e.tenant_id
  AND consent.project_id=e.project_id AND consent.id=e.consent_id
 WHERE e.tenant_id=p_tenant AND e.project_id=p_project AND e.id=p_event
  AND e.learn AND d.document_type='EditingRecipeV3'
  AND consent.status='granted' AND consent.epoch=e.consent_epoch
  AND consent.person_id IS NOT DISTINCT FROM e.person_id
  AND consent.purpose=CASE WHEN e.person_id IS NULL THEN
   'learning_photographer' ELSE 'learning_person' END
  AND (e.person_id IS NULL OR EXISTS(SELECT 1 FROM photo.project_persons person
   WHERE person.tenant_id=e.tenant_id AND person.project_id=e.project_id
    AND person.id=e.person_id AND person.state='active'))
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND (p_side='base' OR
   (approval.id IS NOT NULL AND resolved.state='ready'
    AND resolved.verified_at IS NOT NULL
    AND resolved.object_version IS NOT NULL
    AND resolved.object_version NOT IN ('','null')
    AND (resolved.expires_at IS NULL OR
     resolved.expires_at>clock_timestamp())));
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_document_source(uuid,uuid,uuid,text,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_document_source(uuid,uuid,uuid,text,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_document_source(uuid,uuid,uuid,text,uuid,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
