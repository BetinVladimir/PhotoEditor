-- V21 admission guards; merge with 00196 only after all link classes are covered.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v2_workflow() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; s photo.snapshots%ROWTYPE;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=NEW.tenant_id AND id=NEW.run_id;
 SELECT * INTO s FROM photo.snapshots WHERE tenant_id=NEW.tenant_id AND id=NEW.input_snapshot_id;
 IF r.id IS NULL OR s.id IS NULL OR r.kind<>'v2' OR s.kind<>'v2'
  OR r.input_snapshot_id<>NEW.input_snapshot_id OR r.failure_policy<>'fail_fast'
  OR r.config_snapshot->>'project_id' IS DISTINCT FROM NEW.project_id::text THEN
  RAISE EXCEPTION 'V2_WORKFLOW_SCOPE_INVALID' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v2_workflow() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v2_workflow() FROM PUBLIC;
CREATE TRIGGER v2_workflows_scope BEFORE INSERT ON photo.v2_workflows
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_workflow();

-- +goose StatementBegin
CREATE FUNCTION photo.guard_v2_stream() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source_project uuid;
BEGIN
 IF TG_OP='UPDATE' AND (NEW.tenant_id,NEW.project_id,NEW.asset_version_id,NEW.stream_type)
  IS DISTINCT FROM (OLD.tenant_id,OLD.project_id,OLD.asset_version_id,OLD.stream_type) THEN
  RAISE EXCEPTION 'V2_STREAM_SCOPE_IMMUTABLE' USING ERRCODE='23514';
 END IF;
 IF NEW.stream_type='draft' THEN
  SELECT a.project_id INTO source_project FROM photo.asset_versions v
   JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   WHERE v.tenant_id=NEW.tenant_id AND v.id=NEW.asset_version_id;
  IF source_project IS DISTINCT FROM NEW.project_id THEN
   RAISE EXCEPTION 'V2_STREAM_SOURCE_INVALID' USING ERRCODE='23514';
  END IF;
 END IF;
 IF NEW.current_document_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM photo.v2_documents d WHERE d.tenant_id=NEW.tenant_id AND d.project_id=NEW.project_id
   AND d.stream_id=NEW.id AND d.id=NEW.current_document_id) THEN
  RAISE EXCEPTION 'V2_STREAM_CURRENT_INVALID' USING ERRCODE='23514';
 END IF;
 IF NEW.approved_document_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM photo.v2_documents d WHERE d.tenant_id=NEW.tenant_id AND d.project_id=NEW.project_id
   AND d.stream_id=NEW.id AND d.id=NEW.approved_document_id) THEN
  RAISE EXCEPTION 'V2_STREAM_APPROVED_INVALID' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v2_stream() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v2_stream() FROM PUBLIC;
CREATE TRIGGER v2_document_streams_scope BEFORE INSERT OR UPDATE ON photo.v2_document_streams
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_stream();
