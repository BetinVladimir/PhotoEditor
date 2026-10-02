-- Enumerate only immutable document refs of one authorized correction event.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_mask_roots(
 p_tenant uuid,p_project uuid,p_event uuid,p_actor uuid,p_auth_session uuid)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_event IS NULL OR
  p_actor IS NULL OR p_auth_session IS NULL OR
  NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_auth_session)
  THEN RAISE EXCEPTION 'V23_TRAIN_MASK_ROOTS_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 RETURN QUERY SELECT DISTINCT link.artifact_id,link.sha256
 FROM photo.correction_events event_row
 JOIN photo.v2_document_refs link ON link.tenant_id=event_row.tenant_id
  AND link.project_id=event_row.project_id
  AND link.document_id IN(event_row.base_document_id,
   event_row.approved_document_id)
 JOIN photo.artifacts artifact ON artifact.tenant_id=link.tenant_id
  AND artifact.id=link.artifact_id AND artifact.sha256=link.sha256
 JOIN photo.consent_grants consent ON consent.tenant_id=event_row.tenant_id
  AND consent.project_id=event_row.project_id
  AND consent.id=event_row.consent_id
 WHERE event_row.tenant_id=p_tenant AND event_row.project_id=p_project
  AND event_row.id=p_event AND event_row.learn
  AND consent.status='granted' AND consent.epoch=event_row.consent_epoch
  AND consent.person_id IS NOT DISTINCT FROM event_row.person_id
  AND consent.purpose=CASE WHEN event_row.person_id IS NULL THEN
   'learning_photographer' ELSE 'learning_person' END
  AND artifact.state='ready' AND artifact.media_type='application/json'
  AND artifact.verified_at IS NOT NULL
  AND artifact.object_version IS NOT NULL
  AND artifact.object_version NOT IN ('','null')
  AND (artifact.expires_at IS NULL OR
   artifact.expires_at>clock_timestamp())
 ORDER BY link.artifact_id,link.sha256;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_mask_roots(uuid,uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_mask_roots(uuid,uuid,uuid,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_mask_roots(uuid,uuid,uuid,uuid,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
