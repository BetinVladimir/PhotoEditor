-- Queue only derived learning artifacts when the grant epoch that admitted
-- their examples is revoked. The ledger is idempotent across shared versions;
-- object deletion is a separate reference-aware maintenance operation.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.enqueue_v23_learning_cleanup() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.status='granted' AND NEW.status='revoked' AND
  OLD.purpose IN('learning_photographer','learning_person') THEN
  INSERT INTO photo.consent_cleanup(tenant_id,project_id,consent_id,
   artifact_id,state,attempts,next_attempt_at)
  SELECT DISTINCT OLD.tenant_id,OLD.project_id,OLD.id,derived.artifact_id,
   'pending',0,clock_timestamp()
  FROM photo.learning_example_links link
  JOIN photo.learning_versions version ON version.tenant_id=link.tenant_id
   AND version.project_id=link.project_id AND version.id=link.version_id
  CROSS JOIN LATERAL (VALUES(version.training_artifact_id),
   (version.model_artifact_id),(version.evaluation_artifact_id))
   AS derived(artifact_id)
  WHERE link.tenant_id=OLD.tenant_id AND link.project_id=OLD.project_id
   AND link.consent_id=OLD.id AND link.consent_epoch=OLD.epoch
  ON CONFLICT(tenant_id,consent_id,artifact_id) DO NOTHING;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.enqueue_v23_learning_cleanup() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enqueue_v23_learning_cleanup() FROM PUBLIC;
CREATE TRIGGER v23_learning_cleanup_enqueue
 AFTER UPDATE OF status,epoch ON photo.consent_grants
 FOR EACH ROW EXECUTE FUNCTION photo.enqueue_v23_learning_cleanup();
