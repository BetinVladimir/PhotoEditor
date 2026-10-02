-- +goose Up
ALTER TABLE photo.reviews ADD COLUMN decision_hash text;
ALTER TABLE photo.reviews ADD COLUMN decision_route text;
ALTER TABLE photo.reviews ADD CONSTRAINT reviews_candidate_hash_format CHECK(candidate_hash ~ '^[0-9a-f]{64}$');
ALTER TABLE photo.reviews ADD CONSTRAINT reviews_decision_hash_format CHECK(decision_hash IS NULL OR decision_hash ~ '^[0-9a-f]{64}$');
ALTER TABLE photo.reviews ADD CONSTRAINT reviews_decision_route_domain CHECK(decision_route IS NULL OR decision_route IN ('repair','base','skip'));
ALTER TABLE photo.reviews ADD CONSTRAINT reviews_pending_empty CHECK(state <> 'pending' OR
 (selection IS NULL AND decided_by IS NULL AND decided_at IS NULL AND decision_hash IS NULL AND decision_route IS NULL));

CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.reviews FOR EACH ROW
 EXECUTE FUNCTION photo.reject_content_update('tenant_id','id','created_at','run_id','task_id','candidate_artifact_id','base_artifact_id','candidate_hash','kind','reasons');

-- +goose StatementBegin
CREATE FUNCTION photo.guard_review_decision() RETURNS trigger LANGUAGE plpgsql
 SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.state <> 'pending' OR NEW.revision <> 1 THEN
   RAISE EXCEPTION 'REVIEW_MUST_START_PENDING' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.state='decided' THEN
  IF NEW IS DISTINCT FROM OLD THEN
   RAISE EXCEPTION 'REVIEW_DECISION_IMMUTABLE' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.state='pending' THEN
  IF NEW IS DISTINCT FROM OLD THEN
   RAISE EXCEPTION 'REVIEW_PENDING_IMMUTABLE' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.state <> 'decided' OR OLD.revision>=9007199254740991 OR NEW.revision<>OLD.revision+1 OR NEW.decision_hash IS NULL THEN
  RAISE EXCEPTION 'INVALID_REVIEW_TRANSITION' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION photo.guard_review_decision() FROM PUBLIC;
CREATE TRIGGER review_decision_guard BEFORE INSERT OR UPDATE ON photo.reviews
 FOR EACH ROW EXECUTE FUNCTION photo.guard_review_decision();

-- API decisions are append-once; retention belongs to trusted maintenance.
REVOKE DELETE ON photo.reviews FROM photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
