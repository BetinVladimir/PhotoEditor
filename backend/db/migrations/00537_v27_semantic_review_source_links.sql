-- +goose Up
ALTER TABLE photo.reviews ADD CONSTRAINT reviews_semantic_source_key UNIQUE(tenant_id,candidate_artifact_id,id);
ALTER TABLE photo.overrides ADD CONSTRAINT overrides_semantic_source_key UNIQUE(tenant_id,asset_version_id,id);
ALTER TABLE photo.semantic_targets ADD CONSTRAINT semantic_target_review_source_fk FOREIGN KEY(tenant_id,candidate_artifact_id,review_id) REFERENCES photo.reviews(tenant_id,candidate_artifact_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.semantic_targets ADD CONSTRAINT semantic_target_override_source_fk FOREIGN KEY(tenant_id,asset_version_id,override_id) REFERENCES photo.overrides(tenant_id,asset_version_id,id) DEFERRABLE INITIALLY DEFERRED;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.guard_semantic_target_state() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.state NOT IN('proposed','unsupported') OR NEW.revision<>1 OR NEW.child_run_id IS NOT NULL OR NEW.batch_id IS NOT NULL
  OR NEW.review_id IS NOT NULL OR NEW.override_id IS NOT NULL OR (NEW.state='proposed' AND NEW.candidate_artifact_id IS NULL) THEN
   RAISE EXCEPTION 'SEMANTIC_TARGET_INITIAL_STATE_INVALID' USING ERRCODE='23514';END IF;
  RETURN NEW;
 END IF;
 IF (NEW.id,NEW.tenant_id,NEW.project_id,NEW.created_at,NEW.intent_id,NEW.asset_version_id,NEW.candidate_artifact_id)
 IS DISTINCT FROM (OLD.id,OLD.tenant_id,OLD.project_id,OLD.created_at,OLD.intent_id,OLD.asset_version_id,OLD.candidate_artifact_id) THEN
  RAISE EXCEPTION 'SEMANTIC_TARGET_BINDING_IMMUTABLE' USING ERRCODE='55000';END IF;
 IF OLD.state='proposed' AND NEW.state IN('queued','rejected') THEN
  IF NEW.batch_id IS NULL OR (NEW.state='queued' AND NEW.child_run_id IS NULL) OR (NEW.state='rejected' AND NEW.child_run_id IS NOT NULL) THEN
   RAISE EXCEPTION 'SEMANTIC_TARGET_DECISION_INVALID' USING ERRCODE='23514';END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.v2_review_batches b JOIN photo.semantic_intents i
   ON i.tenant_id=b.tenant_id AND i.project_id=b.project_id AND i.workflow_id=b.workflow_id
   WHERE b.tenant_id=NEW.tenant_id AND b.project_id=NEW.project_id AND b.id=NEW.batch_id AND i.id=NEW.intent_id
   AND b.action IN('apply','decide') AND (NEW.state='rejected' OR (b.action='apply' AND b.child_run_id=NEW.child_run_id))) THEN
   RAISE EXCEPTION 'SEMANTIC_TARGET_BATCH_SCOPE_INVALID' USING ERRCODE='23514';END IF;
 ELSIF OLD.state='queued' AND NEW.state IN('applied','failed','cancelled') THEN
  IF (NEW.child_run_id,NEW.batch_id,NEW.review_id,NEW.override_id) IS DISTINCT FROM
  (OLD.child_run_id,OLD.batch_id,OLD.review_id,OLD.override_id) THEN
   RAISE EXCEPTION 'SEMANTIC_TARGET_CHILD_BINDING_IMMUTABLE' USING ERRCODE='55000';END IF;
 ELSE RAISE EXCEPTION 'SEMANTIC_TARGET_STATE_CONFLICT' USING ERRCODE='55000';END IF;
 IF OLD.revision=9223372036854775807 OR NEW.revision NOT IN(OLD.revision,OLD.revision+1) THEN
  RAISE EXCEPTION 'SEMANTIC_TARGET_REVISION_CONFLICT' USING ERRCODE='55000';END IF;
 NEW.revision:=OLD.revision+1;NEW.updated_at:=clock_timestamp();RETURN NEW;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
