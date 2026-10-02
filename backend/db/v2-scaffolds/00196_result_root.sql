-- V2 success carries one root document. Nested uploads are adopted separately;
-- they must never be smuggled through the legacy TaskResult.outputs envelope.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v2_result_root() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task_row photo.tasks%ROWTYPE; run_row photo.runs%ROWTYPE; root jsonb;
BEGIN
 SELECT * INTO task_row FROM photo.tasks WHERE tenant_id=NEW.tenant_id AND id=NEW.task_id;
 IF NOT FOUND THEN RETURN NEW; END IF;
 SELECT * INTO run_row FROM photo.runs WHERE tenant_id=task_row.tenant_id AND id=task_row.run_id;
 IF run_row.kind<>'v2' THEN RETURN NEW; END IF;
 IF task_row.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=run_row.tenant_id
   AND w.run_id=run_row.id AND w.renderer_version='v2.2') THEN
  RAISE EXCEPTION 'V2_RENDERER_MISMATCH' USING ERRCODE='23514';
 END IF;
 IF jsonb_typeof(NEW.outputs)<>'array' OR jsonb_array_length(NEW.outputs)<>1 THEN
  RAISE EXCEPTION 'V2_ROOT_REQUIRED' USING ERRCODE='23514';
 END IF;
 root:=NEW.outputs->0;
 IF jsonb_typeof(root)<>'object' OR (root-'artifact_id'-'sha256')<>'{}'::jsonb
  OR root->>'artifact_id' IS NULL OR root->>'sha256' IS NULL
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a
   JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
   WHERE a.tenant_id=NEW.tenant_id AND a.id=(root->>'artifact_id')::uuid
   AND a.sha256=root->>'sha256' AND a.output_role='document' AND a.state='ready'
   AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
   AND a.created_by_task_id=task_row.id AND a.produced_attempt=NEW.attempt
   AND a.produced_fence=NEW.fencing_token AND u.state='ready' AND u.role='document'
   AND u.task_id=task_row.id AND u.attempt=NEW.attempt AND u.fence=NEW.fencing_token) THEN
  RAISE EXCEPTION 'V2_ROOT_UNVERIFIED' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v2_result_root() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v2_result_root() FROM PUBLIC;
CREATE TRIGGER v2_result_root_guard BEFORE INSERT ON photo.task_results
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_result_root();
