-- Schema scaffold only; add functions, triggers, GC, purge and checks before migration.

CREATE TABLE photo.v2_workflows (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  run_id uuid NOT NULL,
  input_snapshot_id uuid NOT NULL,
  workflow_type text NOT NULL CHECK (length(workflow_type) BETWEEN 1 AND 256) CHECK (workflow_type IN ('session','learning','capture','semantic','delivery','interchange','provenance','preview')),
  renderer_version text NOT NULL CHECK (length(renderer_version) BETWEEN 1 AND 256),
  policy_sha text NOT NULL CHECK (policy_sha ~ '^[0-9a-f]{64}$'),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,run_id)
);

CREATE TABLE photo.v2_documents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  stream_id uuid NOT NULL,
  version_no bigint NOT NULL CHECK (version_no > 0),
  document_type text NOT NULL CHECK (length(document_type) BETWEEN 1 AND 256) CHECK (document_type IN ('EditingRecipeV3','LayerStack','ShotList')),
  artifact_id uuid NOT NULL,
  sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,stream_id,version_no)
);

CREATE TABLE photo.v2_document_streams (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  asset_version_id uuid,
  stream_type text NOT NULL CHECK (length(stream_type) BETWEEN 1 AND 256) CHECK (stream_type IN ('draft','shot_list')),
  current_document_id uuid,
  approved_document_id uuid,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK ((stream_type='draft') = (asset_version_id IS NOT NULL))
);

CREATE TABLE photo.v2_document_refs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  document_id uuid NOT NULL,
  artifact_id uuid NOT NULL,
  sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  purpose text NOT NULL CHECK (length(purpose) BETWEEN 1 AND 256),
  ordinal integer NOT NULL CHECK (ordinal >= 0),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,document_id,purpose,ordinal)
);

CREATE TABLE photo.v2_artifact_links (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  workflow_id uuid NOT NULL,
  artifact_id uuid NOT NULL,
  sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  purpose text NOT NULL CHECK (length(purpose) BETWEEN 1 AND 256),
  ordinal integer NOT NULL CHECK (ordinal >= 0),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,workflow_id,purpose,ordinal)
);

CREATE TABLE photo.model_qualifications (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  operation text NOT NULL CHECK (length(operation) BETWEEN 1 AND 256),
  operation_version text NOT NULL CHECK (length(operation_version) BETWEEN 1 AND 256),
  model_lock_sha text NOT NULL CHECK (model_lock_sha ~ '^[0-9a-f]{64}$'),
  backend text NOT NULL CHECK (length(backend) BETWEEN 1 AND 256) CHECK (backend IN ('cpu','cuda','mps')),
  device_profile_sha text NOT NULL CHECK (device_profile_sha ~ '^[0-9a-f]{64}$'),
  report_artifact_id uuid NOT NULL,
  status text NOT NULL CHECK (length(status) BETWEEN 1 AND 256) CHECK (status IN ('passed','failed')),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE photo.v2_document_approvals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  document_id uuid NOT NULL,
  actor_id uuid NOT NULL,
  recipe_sha text NOT NULL CHECK (recipe_sha ~ '^[0-9a-f]{64}$'),
  resolved_artifact_id uuid NOT NULL,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,document_id)
);

CREATE TABLE photo.v2_task_inputs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  task_id uuid NOT NULL,
  artifact_id uuid NOT NULL,
  sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  contract text NOT NULL CHECK (length(contract) BETWEEN 1 AND 256),
  ordinal integer NOT NULL CHECK (ordinal >= 0),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,task_id,artifact_id),
  UNIQUE (tenant_id,task_id,ordinal)
);

CREATE TABLE photo.v2_review_batches (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  workflow_id uuid NOT NULL,
  child_run_id uuid,
  decisions_artifact_id uuid NOT NULL,
  actor_id uuid NOT NULL,
  action text NOT NULL CHECK (length(action) BETWEEN 1 AND 256) CHECK (action IN ('apply','decide','close')),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);

ALTER TABLE photo.v2_workflows ADD FOREIGN KEY (tenant_id,run_id) REFERENCES photo.runs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_workflows (tenant_id,run_id);

ALTER TABLE photo.v2_workflows ADD FOREIGN KEY (tenant_id,input_snapshot_id) REFERENCES photo.snapshots(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_workflows (tenant_id,input_snapshot_id);

ALTER TABLE photo.v2_documents ADD FOREIGN KEY (tenant_id,project_id,stream_id) REFERENCES photo.v2_document_streams(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_documents (tenant_id,project_id,stream_id);

ALTER TABLE photo.v2_documents ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_documents (tenant_id,artifact_id);

ALTER TABLE photo.v2_document_streams ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_document_streams (tenant_id,asset_version_id);

ALTER TABLE photo.v2_document_streams ADD FOREIGN KEY (tenant_id,project_id,current_document_id) REFERENCES photo.v2_documents(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_document_streams (tenant_id,project_id,current_document_id);

ALTER TABLE photo.v2_document_streams ADD FOREIGN KEY (tenant_id,project_id,approved_document_id) REFERENCES photo.v2_documents(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_document_streams (tenant_id,project_id,approved_document_id);

ALTER TABLE photo.v2_document_refs ADD FOREIGN KEY (tenant_id,project_id,document_id) REFERENCES photo.v2_documents(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_document_refs (tenant_id,project_id,document_id);

ALTER TABLE photo.v2_document_refs ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_document_refs (tenant_id,artifact_id);

ALTER TABLE photo.v2_artifact_links ADD FOREIGN KEY (tenant_id,project_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_artifact_links (tenant_id,project_id,workflow_id);

ALTER TABLE photo.v2_artifact_links ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_artifact_links (tenant_id,artifact_id);

ALTER TABLE photo.model_qualifications ADD FOREIGN KEY (tenant_id,report_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.model_qualifications (tenant_id,report_artifact_id);

ALTER TABLE photo.v2_document_approvals ADD FOREIGN KEY (tenant_id,project_id,document_id) REFERENCES photo.v2_documents(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_document_approvals (tenant_id,project_id,document_id);

ALTER TABLE photo.v2_document_approvals ADD FOREIGN KEY (actor_id) REFERENCES photo.users(id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_document_approvals (actor_id);

ALTER TABLE photo.v2_document_approvals ADD FOREIGN KEY (tenant_id,resolved_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_document_approvals (tenant_id,resolved_artifact_id);

ALTER TABLE photo.v2_task_inputs ADD FOREIGN KEY (tenant_id,task_id) REFERENCES photo.tasks(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_task_inputs (tenant_id,task_id);

ALTER TABLE photo.v2_task_inputs ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_task_inputs (tenant_id,artifact_id);

ALTER TABLE photo.v2_review_batches ADD FOREIGN KEY (tenant_id,project_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_review_batches (tenant_id,project_id,workflow_id);

ALTER TABLE photo.v2_review_batches ADD FOREIGN KEY (tenant_id,child_run_id) REFERENCES photo.runs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_review_batches (tenant_id,child_run_id);

ALTER TABLE photo.v2_review_batches ADD FOREIGN KEY (tenant_id,decisions_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_review_batches (tenant_id,decisions_artifact_id);

ALTER TABLE photo.v2_review_batches ADD FOREIGN KEY (actor_id) REFERENCES photo.users(id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.v2_review_batches (actor_id);

ALTER TABLE photo.v2_workflows ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.v2_workflows FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.v2_workflows TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.v2_workflows TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.v2_workflows FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.v2_workflows TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.v2_workflows TO photo_fn_owner;

CREATE INDEX ON photo.v2_workflows (tenant_id,project_id,created_at,id);

ALTER TABLE photo.v2_documents ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.v2_documents FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.v2_documents TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.v2_documents TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.v2_documents FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.v2_documents TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.v2_documents TO photo_fn_owner;

CREATE INDEX ON photo.v2_documents (tenant_id,project_id,created_at,id);

ALTER TABLE photo.v2_document_streams ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.v2_document_streams FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.v2_document_streams TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.v2_document_streams TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.v2_document_streams FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.v2_document_streams TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.v2_document_streams TO photo_fn_owner;

CREATE INDEX ON photo.v2_document_streams (tenant_id,project_id,created_at,id);

ALTER TABLE photo.v2_document_refs ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.v2_document_refs FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.v2_document_refs TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.v2_document_refs TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.v2_document_refs FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.v2_document_refs TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.v2_document_refs TO photo_fn_owner;

CREATE INDEX ON photo.v2_document_refs (tenant_id,project_id,created_at,id);

ALTER TABLE photo.v2_artifact_links ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.v2_artifact_links FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.v2_artifact_links TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.v2_artifact_links TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.v2_artifact_links FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.v2_artifact_links TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.v2_artifact_links TO photo_fn_owner;

CREATE INDEX ON photo.v2_artifact_links (tenant_id,project_id,created_at,id);

ALTER TABLE photo.model_qualifications ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.model_qualifications FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.model_qualifications TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.model_qualifications TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.model_qualifications FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.model_qualifications TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.model_qualifications TO photo_fn_owner;

CREATE INDEX ON photo.model_qualifications (tenant_id,project_id,created_at,id);

ALTER TABLE photo.v2_document_approvals ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.v2_document_approvals FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.v2_document_approvals TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.v2_document_approvals TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.v2_document_approvals FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.v2_document_approvals TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.v2_document_approvals TO photo_fn_owner;

CREATE INDEX ON photo.v2_document_approvals (tenant_id,project_id,created_at,id);

ALTER TABLE photo.v2_task_inputs ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.v2_task_inputs FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.v2_task_inputs TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.v2_task_inputs TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.v2_task_inputs FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.v2_task_inputs TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.v2_task_inputs TO photo_fn_owner;

CREATE INDEX ON photo.v2_task_inputs (tenant_id,project_id,created_at,id);

ALTER TABLE photo.v2_review_batches ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.v2_review_batches FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.v2_review_batches TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.v2_review_batches TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.v2_review_batches FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.v2_review_batches TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.v2_review_batches TO photo_fn_owner;

CREATE INDEX ON photo.v2_review_batches (tenant_id,project_id,created_at,id);
