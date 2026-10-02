-- Merge into 00196 after the V21 transaction functions and deletion lifecycle exist.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v2_immutable() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 RAISE EXCEPTION 'V2_IMMUTABLE' USING ERRCODE='55000';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v2_immutable() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v2_immutable() FROM PUBLIC;

CREATE TRIGGER v2_workflows_immutable BEFORE UPDATE ON photo.v2_workflows FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
CREATE TRIGGER v2_documents_immutable BEFORE UPDATE ON photo.v2_documents FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
CREATE TRIGGER v2_document_refs_immutable BEFORE UPDATE ON photo.v2_document_refs FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
CREATE TRIGGER v2_artifact_links_immutable BEFORE UPDATE ON photo.v2_artifact_links FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
CREATE TRIGGER model_qualifications_immutable BEFORE UPDATE ON photo.model_qualifications FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
CREATE TRIGGER v2_document_approvals_immutable BEFORE UPDATE ON photo.v2_document_approvals FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
CREATE TRIGGER v2_task_inputs_immutable BEFORE UPDATE ON photo.v2_task_inputs FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
CREATE TRIGGER v2_review_batches_immutable BEFORE UPDATE ON photo.v2_review_batches FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();

-- +goose StatementBegin
CREATE FUNCTION photo.bump_v2_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.revision >= 9223372036854775806 THEN
  RAISE EXCEPTION 'V2_REVISION_EXHAUSTED' USING ERRCODE='22003';
 END IF;
 NEW.revision := OLD.revision + 1;
 NEW.updated_at := statement_timestamp();
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.bump_v2_revision() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.bump_v2_revision() FROM PUBLIC;
CREATE TRIGGER v2_document_streams_revision BEFORE UPDATE ON photo.v2_document_streams
 FOR EACH ROW EXECUTE FUNCTION photo.bump_v2_revision();
