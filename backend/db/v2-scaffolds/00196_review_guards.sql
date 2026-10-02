-- Approval/review rows are immutable evidence; validate relational authority at insert.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v2_approval() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT EXISTS(
  SELECT 1 FROM photo.v2_documents d
  JOIN photo.v2_document_streams s ON s.tenant_id=d.tenant_id AND s.id=d.stream_id
  WHERE d.tenant_id=NEW.tenant_id AND d.project_id=NEW.project_id AND d.id=NEW.document_id
   AND d.document_type='EditingRecipeV3' AND d.sha256=NEW.recipe_sha
   AND s.stream_type='draft' AND s.current_document_id=d.id
 ) OR NOT EXISTS(
  SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=NEW.tenant_id AND a.id=NEW.resolved_artifact_id
   AND a.state='ready' AND a.object_version IS NOT NULL AND a.verified_at IS NOT NULL
 ) OR NOT EXISTS(
  SELECT 1 FROM photo.memberships m WHERE m.tenant_id=NEW.tenant_id AND m.user_id=NEW.actor_id
   AND m.status='active' AND m.role IN('owner','admin','editor')
 ) THEN
  RAISE EXCEPTION 'V2_APPROVAL_INVALID' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v2_approval() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v2_approval() FROM PUBLIC;
CREATE TRIGGER v2_document_approvals_guard BEFORE INSERT ON photo.v2_document_approvals
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_approval();

-- +goose StatementBegin
CREATE FUNCTION photo.guard_v2_review_batch() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT EXISTS(
  SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=NEW.tenant_id
   AND w.project_id=NEW.project_id AND w.id=NEW.workflow_id
 ) OR NOT EXISTS(
  SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=NEW.tenant_id AND a.id=NEW.decisions_artifact_id
   AND a.state='ready' AND a.object_version IS NOT NULL AND a.verified_at IS NOT NULL
 ) OR NOT EXISTS(
  SELECT 1 FROM photo.memberships m WHERE m.tenant_id=NEW.tenant_id AND m.user_id=NEW.actor_id
   AND m.status='active' AND m.role IN('owner','admin','editor')
 ) OR (NEW.child_run_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM photo.runs r WHERE r.tenant_id=NEW.tenant_id AND r.id=NEW.child_run_id
   AND r.kind IN('apply','v2') AND r.config_snapshot->>'project_id'=NEW.project_id::text
 )) THEN
  RAISE EXCEPTION 'V2_REVIEW_BATCH_INVALID' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v2_review_batch() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v2_review_batch() FROM PUBLIC;
CREATE TRIGGER v2_review_batches_guard BEFORE INSERT ON photo.v2_review_batches
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_review_batch();
