-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.guard_semantic_intent_immutable() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN RAISE EXCEPTION 'SEMANTIC_INTENT_IMMUTABLE' USING ERRCODE='55000';END $$;
ALTER FUNCTION photo.guard_semantic_intent_immutable() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_semantic_intent_immutable() FROM PUBLIC;
CREATE TRIGGER semantic_intent_immutable BEFORE UPDATE ON photo.semantic_intents FOR EACH ROW EXECUTE FUNCTION photo.guard_semantic_intent_immutable();

CREATE FUNCTION photo.guard_semantic_target_state() RETURNS trigger
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
 ELSIF OLD.state='queued' AND NEW.state IN('applied','failed','cancelled') THEN
  IF (NEW.child_run_id,NEW.batch_id,NEW.review_id,NEW.override_id) IS DISTINCT FROM
  (OLD.child_run_id,OLD.batch_id,OLD.review_id,OLD.override_id) THEN
   RAISE EXCEPTION 'SEMANTIC_TARGET_CHILD_BINDING_IMMUTABLE' USING ERRCODE='55000';END IF;
 ELSE RAISE EXCEPTION 'SEMANTIC_TARGET_STATE_CONFLICT' USING ERRCODE='55000';END IF;
 IF OLD.revision=9223372036854775807 OR NEW.revision NOT IN(OLD.revision,OLD.revision+1) THEN
  RAISE EXCEPTION 'SEMANTIC_TARGET_REVISION_CONFLICT' USING ERRCODE='55000';END IF;
 NEW.revision:=OLD.revision+1;NEW.updated_at:=clock_timestamp();RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_semantic_target_state() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_semantic_target_state() FROM PUBLIC;
CREATE TRIGGER semantic_target_state_guard BEFORE INSERT OR UPDATE ON photo.semantic_targets FOR EACH ROW EXECUTE FUNCTION photo.guard_semantic_target_state();
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
