-- Parent run for one immutable selection exported through one or more presets.
-- +goose Up
CREATE TABLE photo.export_batches(
 tenant_id uuid NOT NULL,run_id uuid NOT NULL,preset_count int NOT NULL,status text NOT NULL DEFAULT 'running',created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,run_id),FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id),CHECK(preset_count BETWEEN 1 AND 16),CHECK(status IN('running','completed','completed_with_errors','failed','cancelled'))
);
CREATE TABLE photo.export_batch_children(
 tenant_id uuid NOT NULL,batch_run_id uuid NOT NULL,child_run_id uuid NOT NULL,download_job_id uuid NOT NULL,preset_id uuid NOT NULL,preset_revision bigint NOT NULL,preset_hash text NOT NULL,
 PRIMARY KEY(tenant_id,batch_run_id,child_run_id),UNIQUE(tenant_id,download_job_id),FOREIGN KEY(tenant_id,batch_run_id) REFERENCES photo.export_batches(tenant_id,run_id),FOREIGN KEY(tenant_id,child_run_id) REFERENCES photo.runs(tenant_id,id),FOREIGN KEY(tenant_id,download_job_id) REFERENCES photo.download_jobs(tenant_id,id),CHECK(preset_hash~'^[0-9a-f]{64}$')
);
ALTER TABLE photo.export_batches ENABLE ROW LEVEL SECURITY;ALTER TABLE photo.export_batches FORCE ROW LEVEL SECURITY;ALTER TABLE photo.export_batch_children ENABLE ROW LEVEL SECURITY;ALTER TABLE photo.export_batch_children FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.export_batches TO photo_api USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);CREATE POLICY trusted_functions ON photo.export_batches TO photo_fn_owner USING(true) WITH CHECK(true);
CREATE POLICY tenant_scope ON photo.export_batch_children TO photo_api USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);CREATE POLICY trusted_functions ON photo.export_batch_children TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE ON photo.export_batches,photo.export_batch_children TO photo_api;GRANT SELECT,INSERT,UPDATE ON photo.export_batches,photo.export_batch_children TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.finalize_export_batch() RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.export_batches%ROWTYPE;target text;
BEGIN
 SELECT * INTO b FROM photo.export_batches x WHERE x.status='running' AND NOT EXISTS(SELECT FROM photo.export_batch_children c JOIN photo.runs r ON r.tenant_id=c.tenant_id AND r.id=c.child_run_id WHERE c.tenant_id=x.tenant_id AND c.batch_run_id=x.run_id AND r.status NOT IN('completed','completed_with_errors','failed','cancelled')) ORDER BY x.created_at LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT CASE WHEN EXISTS(SELECT FROM photo.export_batch_children c JOIN photo.runs r ON r.tenant_id=c.tenant_id AND r.id=c.child_run_id WHERE c.tenant_id=b.tenant_id AND c.batch_run_id=b.run_id AND r.status='failed') THEN 'failed' WHEN EXISTS(SELECT FROM photo.export_batch_children c JOIN photo.runs r ON r.tenant_id=c.tenant_id AND r.id=c.child_run_id WHERE c.tenant_id=b.tenant_id AND c.batch_run_id=b.run_id AND r.status='cancelled') THEN 'cancelled' WHEN EXISTS(SELECT FROM photo.export_batch_children c JOIN photo.runs r ON r.tenant_id=c.tenant_id AND r.id=c.child_run_id WHERE c.tenant_id=b.tenant_id AND c.batch_run_id=b.run_id AND r.status='completed_with_errors') THEN 'completed_with_errors' ELSE 'completed' END INTO target;
 UPDATE photo.export_batches SET status=target WHERE tenant_id=b.tenant_id AND run_id=b.run_id;UPDATE photo.runs SET status=target,revision=revision+1 WHERE tenant_id=b.tenant_id AND id=b.run_id AND status='running';RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_export_batch() OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.finalize_export_batch() FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.finalize_export_batch() TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
