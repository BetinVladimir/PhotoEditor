-- Relational closure checks for V21; content validation stays in trusted services.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v2_artifact_link() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE artifact photo.artifacts%ROWTYPE; parent_ok boolean;
BEGIN
 SELECT * INTO artifact FROM photo.artifacts a WHERE a.tenant_id=NEW.tenant_id AND a.id=NEW.artifact_id;
 IF artifact.id IS NULL OR artifact.sha256<>NEW.sha256 OR artifact.state<>'ready'
  OR artifact.object_version IS NULL OR artifact.verified_at IS NULL
  OR (artifact.expires_at IS NOT NULL AND artifact.expires_at<=clock_timestamp()) THEN
  RAISE EXCEPTION 'V2_ARTIFACT_UNVERIFIED' USING ERRCODE='23514';
 END IF;
 CASE TG_TABLE_NAME
  WHEN 'v2_documents' THEN
   SELECT EXISTS(SELECT 1 FROM photo.v2_document_streams s
    WHERE s.tenant_id=NEW.tenant_id AND s.project_id=NEW.project_id AND s.id=NEW.stream_id)
    INTO parent_ok;
  WHEN 'v2_document_refs' THEN
   SELECT EXISTS(SELECT 1 FROM photo.v2_documents d
    WHERE d.tenant_id=NEW.tenant_id AND d.project_id=NEW.project_id AND d.id=NEW.document_id)
    INTO parent_ok;
  WHEN 'v2_artifact_links' THEN
   SELECT EXISTS(SELECT 1 FROM photo.v2_workflows w
    WHERE w.tenant_id=NEW.tenant_id AND w.project_id=NEW.project_id AND w.id=NEW.workflow_id)
    INTO parent_ok;
  WHEN 'v2_task_inputs' THEN
   SELECT EXISTS(SELECT 1 FROM photo.tasks t
    JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
    WHERE t.tenant_id=NEW.tenant_id AND t.id=NEW.task_id AND w.project_id=NEW.project_id)
    INTO parent_ok;
  ELSE
   RAISE EXCEPTION 'V2_ARTIFACT_LINK_TABLE_INVALID' USING ERRCODE='23514';
 END CASE;
 IF NOT parent_ok THEN
  RAISE EXCEPTION 'V2_ARTIFACT_LINK_SCOPE_INVALID' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v2_artifact_link() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v2_artifact_link() FROM PUBLIC;
CREATE TRIGGER v2_documents_artifact_guard BEFORE INSERT ON photo.v2_documents
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_artifact_link();
CREATE TRIGGER v2_document_refs_artifact_guard BEFORE INSERT ON photo.v2_document_refs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_artifact_link();
CREATE TRIGGER v2_artifact_links_artifact_guard BEFORE INSERT ON photo.v2_artifact_links
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_artifact_link();
CREATE TRIGGER v2_task_inputs_artifact_guard BEFORE INSERT ON photo.v2_task_inputs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_artifact_link();
