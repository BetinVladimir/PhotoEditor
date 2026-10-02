-- +goose Up
CREATE TABLE photo.ingest_progress (
 tenant_id uuid NOT NULL,run_id uuid PRIMARY KEY,
 phase text NOT NULL CHECK(phase IN ('inspection','decode','catalog','complete')),
 item_count bigint NOT NULL CHECK(item_count BETWEEN 1 AND 10000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id)
);
ALTER TABLE photo.ingest_progress ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.ingest_progress FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.ingest_progress TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE ON photo.ingest_progress TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.seal_ingest_inspection(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;progress photo.ingest_progress%ROWTYPE;expected bigint;actual bigint;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'ingest' OR r.cancel_requested OR r.status NOT IN ('queued','running') THEN RETURN false;END IF;
 SELECT * INTO progress FROM photo.ingest_progress WHERE tenant_id=p_tenant AND run_id=p_run;
 IF FOUND THEN RETURN progress.phase='inspection' AND r.graph_sealed;END IF;
 IF r.graph_sealed OR r.revision>=9007199254740991 THEN RETURN false;END IF;
 SELECT item_count INTO expected FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id;
 SELECT count(*) INTO actual FROM photo.snapshot_items WHERE tenant_id=p_tenant AND snapshot_id=r.input_snapshot_id;
 IF expected IS NULL OR expected NOT BETWEEN 1 AND 10000 OR actual<>expected THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.ingest_node_plans WHERE tenant_id=p_tenant AND run_id=p_run)<>3*expected
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>2*expected THEN RETURN false;END IF;
 IF EXISTS(SELECT FROM photo.snapshot_items s CROSS JOIN (VALUES('asset.verify'),('asset.inspect'),('asset.decode')) op(name)
  WHERE s.tenant_id=p_tenant AND s.snapshot_id=r.input_snapshot_id AND NOT EXISTS(
   SELECT FROM photo.ingest_node_plans n JOIN photo.backend_documents d ON d.tenant_id=n.tenant_id AND d.run_id=n.run_id AND d.owner_id=n.id
   JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
   WHERE n.tenant_id=s.tenant_id AND n.run_id=p_run AND n.ordinal=s.ordinal AND n.operation=op.name
   AND n.source_artifact_id=s.artifact_id AND n.source_sha=s.sha256 AND d.purpose='task_params' AND d.state='ready'
   AND a.state='ready' AND a.sha256=n.params_sha)) THEN RETURN false;END IF;
 IF EXISTS(SELECT FROM photo.ingest_node_plans n WHERE n.tenant_id=p_tenant AND n.run_id=p_run AND n.operation IN ('asset.verify','asset.inspect')
  AND NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=n.tenant_id AND t.run_id=n.run_id AND t.id=n.id
   AND t.node_key=n.node_key AND t.operation=n.operation AND t.state='blocked' AND NOT t.cancel_requested AND t.terminal_reason IS NULL)) THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)<>expected THEN RETURN false;END IF;
 IF EXISTS(SELECT FROM photo.ingest_node_plans n JOIN photo.ingest_node_plans v ON v.tenant_id=n.tenant_id AND v.run_id=n.run_id AND v.ordinal=n.ordinal AND v.operation='asset.verify'
  WHERE n.tenant_id=p_tenant AND n.run_id=p_run AND n.operation='asset.inspect' AND NOT EXISTS(
   SELECT FROM photo.task_edges e WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.prerequisite_id=v.id AND e.dependent_id=n.id AND e.condition='success')) THEN RETURN false;END IF;
 IF EXISTS(SELECT FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run AND state<>'closed') THEN RETURN false;END IF;
 INSERT INTO photo.ingest_progress(tenant_id,run_id,phase,item_count) VALUES(p_tenant,p_run,'inspection',expected);
 UPDATE photo.runs SET graph_sealed=true,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seal_ingest_inspection(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_ingest_inspection(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_ingest_inspection(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.guard_ingest_completion() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.kind='ingest' AND NEW.status IN ('completed','completed_with_errors')
 AND NOT EXISTS(SELECT FROM photo.ingest_progress p WHERE p.tenant_id=NEW.tenant_id AND p.run_id=NEW.id AND p.phase='complete') THEN
  RAISE EXCEPTION 'INGEST_CONTINUATION_INCOMPLETE' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_ingest_completion() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_ingest_completion() FROM PUBLIC;
CREATE TRIGGER ingest_completion_guard BEFORE INSERT OR UPDATE OF status ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.guard_ingest_completion();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
