-- Reference schema, not a deployable migration. Function/trigger contracts: 03, 09, 11, 13.

-- Apply only after generating migration functions and all acceptance tests.

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

CREATE TABLE photo.shoot_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  collection_id uuid NOT NULL,
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  state text NOT NULL CHECK (length(state) BETWEEN 1 AND 256) CHECK (state IN ('open','closed','archived')),
  policy jsonb NOT NULL CHECK (jsonb_typeof(policy) = 'object') CHECK (octet_length(policy::text) <= 1048576),
  capture_policy jsonb CHECK (jsonb_typeof(capture_policy) = 'object') CHECK (octet_length(capture_policy::text) <= 1048576),
  consent_epoch bigint NOT NULL CHECK (consent_epoch > 0),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE photo.session_analyses (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  session_id uuid NOT NULL,
  workflow_id uuid NOT NULL,
  snapshot_id uuid NOT NULL,
  plan_artifact_id uuid NOT NULL,
  policy_sha text NOT NULL CHECK (policy_sha ~ '^[0-9a-f]{64}$'),
  model_lock_sha text NOT NULL CHECK (model_lock_sha ~ '^[0-9a-f]{64}$'),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,workflow_id)
);

CREATE TABLE photo.scene_groups (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  analysis_id uuid NOT NULL,
  ordinal integer NOT NULL CHECK (ordinal >= 0),
  reference_version_id uuid NOT NULL,
  confidence double precision NOT NULL CHECK (confidence BETWEEN 0 AND 1 AND confidence <> 'NaN'::double precision),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,analysis_id,ordinal)
);

CREATE TABLE photo.scene_members (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  group_id uuid NOT NULL,
  asset_version_id uuid NOT NULL,
  snapshot_ordinal integer NOT NULL CHECK (snapshot_ordinal >= 0),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,group_id,asset_version_id)
);

CREATE TABLE photo.project_persons (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  label text NOT NULL CHECK (length(label) BETWEEN 1 AND 256),
  state text NOT NULL CHECK (length(state) BETWEEN 1 AND 256) CHECK (state IN ('active','revoked')),
  consent_epoch bigint NOT NULL CHECK (consent_epoch > 0),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE photo.person_observations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  person_id uuid NOT NULL,
  asset_version_id uuid NOT NULL,
  face_id uuid NOT NULL,
  evidence_artifact_id uuid NOT NULL,
  confirmed_by uuid,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,asset_version_id,face_id)
);

CREATE TABLE photo.consent_grants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  purpose text NOT NULL CHECK (length(purpose) BETWEEN 1 AND 256) CHECK (purpose IN ('identity_matching','learning_photographer','learning_person')),
  person_id uuid,
  status text NOT NULL CHECK (length(status) BETWEEN 1 AND 256) CHECK (status IN ('granted','revoked')),
  epoch bigint NOT NULL CHECK (epoch > 0),
  policy_version text NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 256),
  actor_id uuid NOT NULL,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK ((purpose='learning_photographer') = (person_id IS NULL))
);

CREATE TABLE photo.consent_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  consent_id uuid NOT NULL,
  epoch bigint NOT NULL CHECK (epoch > 0),
  action text NOT NULL CHECK (length(action) BETWEEN 1 AND 256) CHECK (action IN ('grant','revoke')),
  actor_id uuid NOT NULL,
  policy_version text NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 256),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,consent_id,epoch)
);

CREATE TABLE photo.workflow_consent_links (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  workflow_id uuid NOT NULL,
  consent_id uuid NOT NULL,
  consent_epoch bigint NOT NULL CHECK (consent_epoch > 0),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,workflow_id,consent_id)
);

CREATE TABLE photo.correction_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  session_id uuid NOT NULL,
  asset_version_id uuid NOT NULL,
  base_document_id uuid NOT NULL,
  approved_document_id uuid NOT NULL,
  changed_paths jsonb NOT NULL CHECK (jsonb_typeof(changed_paths) = 'array') CHECK (octet_length(changed_paths::text) <= 1048576),
  reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 256) CHECK (reason IN ('exposure','color','crop','retouch','selection','other')),
  learn boolean NOT NULL,
  consent_id uuid,
  consent_epoch bigint CHECK (consent_epoch > 0),
  approved_by uuid NOT NULL,
  client_event_id uuid NOT NULL,
  person_id uuid,
  scene_group_id uuid,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,client_event_id),
  CHECK ((learn AND consent_id IS NOT NULL AND consent_epoch IS NOT NULL) OR (NOT learn AND consent_id IS NULL AND consent_epoch IS NULL))
);

CREATE TABLE photo.learning_profiles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  mode text NOT NULL CHECK (length(mode) BETWEEN 1 AND 256) CHECK (mode IN ('photographer','person')),
  person_id uuid,
  created_by uuid NOT NULL,
  active_version_id uuid,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK ((mode='person') = (person_id IS NOT NULL))
);

CREATE TABLE photo.learning_versions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  profile_id uuid NOT NULL,
  version_no bigint NOT NULL CHECK (version_no > 0),
  workflow_id uuid NOT NULL,
  training_artifact_id uuid NOT NULL,
  model_artifact_id uuid NOT NULL,
  evaluation_artifact_id uuid NOT NULL,
  parent_version_id uuid,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,profile_id,version_no),
  UNIQUE (tenant_id,workflow_id)
);

CREATE TABLE photo.version_eligibility (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version_id uuid NOT NULL,
  status text NOT NULL CHECK (length(status) BETWEEN 1 AND 256) CHECK (status IN ('eligible','rejected','needs_review','disabled')),
  reason text CHECK (length(reason) BETWEEN 1 AND 256),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,version_id)
);

CREATE TABLE photo.learning_example_links (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  version_id uuid NOT NULL,
  event_id uuid NOT NULL,
  consent_id uuid NOT NULL,
  consent_epoch bigint NOT NULL CHECK (consent_epoch > 0),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,version_id,event_id)
);

CREATE TABLE photo.model_use_links (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  document_id uuid NOT NULL,
  version_id uuid NOT NULL,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,document_id,version_id)
);

CREATE TABLE photo.profile_activations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  profile_id uuid NOT NULL,
  version_id uuid NOT NULL,
  actor_id uuid NOT NULL,
  reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 256) CHECK (reason IN ('promote','rollback')),
  previous_version_id uuid,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE photo.consent_cleanup (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  consent_id uuid NOT NULL,
  artifact_id uuid NOT NULL,
  state text NOT NULL CHECK (length(state) BETWEEN 1 AND 256) CHECK (state IN ('pending','deleting','deleted','blocked_shared')),
  attempts integer NOT NULL CHECK (attempts >= 0),
  next_attempt_at timestamptz NOT NULL,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,consent_id,artifact_id)
);

CREATE TABLE photo.capture_devices (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  label text NOT NULL CHECK (length(label) BETWEEN 1 AND 256),
  public_key text NOT NULL CHECK (length(public_key) BETWEEN 1 AND 256),
  credential_epoch bigint NOT NULL CHECK (credential_epoch > 0),
  created_by uuid NOT NULL,
  state text NOT NULL CHECK (length(state) BETWEEN 1 AND 256) CHECK (state IN ('active','revoked')),
  last_seen_at timestamptz,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE photo.capture_challenges (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  device_id uuid NOT NULL,
  nonce_ciphertext bytea NOT NULL,
  key_version text NOT NULL CHECK (length(key_version) BETWEEN 1 AND 256),
  nonce_hash text NOT NULL CHECK (nonce_hash ~ '^[0-9a-f]{64}$'),
  credential_epoch bigint NOT NULL CHECK (credential_epoch > 0),
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,nonce_hash)
);

CREATE TABLE photo.capture_receipts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  device_id uuid NOT NULL,
  session_id uuid NOT NULL,
  sequence bigint NOT NULL CHECK (sequence >= 0),
  content_sha text NOT NULL CHECK (content_sha ~ '^[0-9a-f]{64}$'),
  upload_id uuid NOT NULL,
  captured_at timestamptz,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,device_id,sequence)
);

CREATE TABLE photo.capture_receipt_assets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  receipt_id uuid NOT NULL,
  asset_version_id uuid NOT NULL,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,receipt_id)
);

CREATE TABLE photo.capture_alerts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  session_id uuid NOT NULL,
  asset_version_id uuid NOT NULL,
  report_artifact_id uuid NOT NULL,
  severity text NOT NULL CHECK (length(severity) BETWEEN 1 AND 256) CHECK (severity IN ('info','warning','critical')),
  receipt_id uuid,
  qa_stale boolean NOT NULL,
  acknowledged_by uuid,
  acknowledged_at timestamptz,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK ((acknowledged_by IS NULL) = (acknowledged_at IS NULL))
);

CREATE TABLE photo.shot_lists (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  session_id uuid NOT NULL,
  document_id uuid NOT NULL,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,session_id)
);

CREATE TABLE photo.semantic_intents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  workflow_id uuid NOT NULL,
  reference_version_id uuid NOT NULL,
  snapshot_id uuid NOT NULL,
  intent_artifact_id uuid NOT NULL,
  intent_sha text NOT NULL CHECK (intent_sha ~ '^[0-9a-f]{64}$'),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,workflow_id)
);

CREATE TABLE photo.semantic_targets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  intent_id uuid NOT NULL,
  child_run_id uuid,
  batch_id uuid,
  asset_version_id uuid NOT NULL,
  candidate_artifact_id uuid,
  review_id uuid,
  override_id uuid,
  state text NOT NULL CHECK (length(state) BETWEEN 1 AND 256) CHECK (state IN ('proposed','queued','rejected','unsupported','applied','failed','cancelled')),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,intent_id,asset_version_id)
);

CREATE TABLE photo.delivery_packages (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  workflow_id uuid NOT NULL,
  snapshot_id uuid NOT NULL,
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  policy_artifact_id uuid NOT NULL,
  manifest_artifact_id uuid,
  published_at timestamptz,
  state text NOT NULL CHECK (length(state) BETWEEN 1 AND 256) CHECK (state IN ('building','review','ready','failed','cancelled')),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,workflow_id),
  CHECK (published_at IS NULL OR (state='ready' AND manifest_artifact_id IS NOT NULL))
);

CREATE TABLE photo.delivery_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  package_id uuid NOT NULL,
  ordinal integer NOT NULL CHECK (ordinal >= 0),
  preset_id uuid NOT NULL,
  preset_sha text NOT NULL CHECK (preset_sha ~ '^[0-9a-f]{64}$'),
  required boolean NOT NULL,
  export_id uuid,
  state text NOT NULL CHECK (length(state) BETWEEN 1 AND 256) CHECK (state IN ('pending','ready','failed','omitted')),
  reason text CHECK (length(reason) BETWEEN 1 AND 256),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,package_id,ordinal,preset_id),
  CHECK (state <> 'ready' OR export_id IS NOT NULL)
);

CREATE TABLE photo.interchange_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  workflow_id uuid NOT NULL,
  direction text NOT NULL CHECK (length(direction) BETWEEN 1 AND 256) CHECK (direction IN ('import','export')),
  format text NOT NULL CHECK (length(format) BETWEEN 1 AND 256) CHECK (format IN ('xmp','photoeditor_bundle','captureone_sidecar')),
  mapping_artifact_id uuid,
  report_artifact_id uuid,
  bundle_artifact_id uuid,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,workflow_id)
);

CREATE TABLE photo.provenance_manifests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  export_id uuid NOT NULL,
  workflow_id uuid NOT NULL,
  manifest_artifact_id uuid NOT NULL,
  signed_artifact_id uuid NOT NULL,
  signer_key_id text NOT NULL CHECK (length(signer_key_id) BETWEEN 1 AND 256),
  signing_policy_sha text NOT NULL CHECK (signing_policy_sha ~ '^[0-9a-f]{64}$'),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,export_id,signing_policy_sha)
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

CREATE TABLE photo.learning_safety_reviews (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  version_id uuid NOT NULL,
  evaluation_sha text NOT NULL CHECK (evaluation_sha ~ '^[0-9a-f]{64}$'),
  actor_id uuid NOT NULL,
  accept boolean NOT NULL,
  reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 256),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE photo.interchange_targets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  job_id uuid NOT NULL,
  entry_id uuid NOT NULL,
  proposed_document_id uuid,
  state text NOT NULL CHECK (length(state) BETWEEN 1 AND 256) CHECK (state IN ('proposed','approved','rejected','unsupported')),
  batch_id uuid,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (tenant_id,job_id,entry_id)
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

ALTER TABLE photo.shoot_sessions ADD FOREIGN KEY (tenant_id,collection_id) REFERENCES photo.collections(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.shoot_sessions (tenant_id,collection_id);

ALTER TABLE photo.session_analyses ADD FOREIGN KEY (tenant_id,project_id,session_id) REFERENCES photo.shoot_sessions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.session_analyses (tenant_id,project_id,session_id);

ALTER TABLE photo.session_analyses ADD FOREIGN KEY (tenant_id,project_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.session_analyses (tenant_id,project_id,workflow_id);

ALTER TABLE photo.session_analyses ADD FOREIGN KEY (tenant_id,snapshot_id) REFERENCES photo.snapshots(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.session_analyses (tenant_id,snapshot_id);

ALTER TABLE photo.session_analyses ADD FOREIGN KEY (tenant_id,plan_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.session_analyses (tenant_id,plan_artifact_id);

ALTER TABLE photo.scene_groups ADD FOREIGN KEY (tenant_id,project_id,analysis_id) REFERENCES photo.session_analyses(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.scene_groups (tenant_id,project_id,analysis_id);

ALTER TABLE photo.scene_groups ADD FOREIGN KEY (tenant_id,reference_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.scene_groups (tenant_id,reference_version_id);

ALTER TABLE photo.scene_members ADD FOREIGN KEY (tenant_id,project_id,group_id) REFERENCES photo.scene_groups(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.scene_members (tenant_id,project_id,group_id);

ALTER TABLE photo.scene_members ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.scene_members (tenant_id,asset_version_id);

ALTER TABLE photo.person_observations ADD FOREIGN KEY (tenant_id,project_id,person_id) REFERENCES photo.project_persons(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.person_observations (tenant_id,project_id,person_id);

ALTER TABLE photo.person_observations ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.person_observations (tenant_id,asset_version_id);

ALTER TABLE photo.person_observations ADD FOREIGN KEY (tenant_id,evidence_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.person_observations (tenant_id,evidence_artifact_id);

ALTER TABLE photo.person_observations ADD FOREIGN KEY (confirmed_by) REFERENCES photo.users(id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.person_observations (confirmed_by);

ALTER TABLE photo.consent_grants ADD FOREIGN KEY (tenant_id,project_id,person_id) REFERENCES photo.project_persons(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.consent_grants (tenant_id,project_id,person_id);

ALTER TABLE photo.consent_grants ADD FOREIGN KEY (actor_id) REFERENCES photo.users(id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.consent_grants (actor_id);

ALTER TABLE photo.consent_events ADD FOREIGN KEY (tenant_id,project_id,consent_id) REFERENCES photo.consent_grants(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.consent_events (tenant_id,project_id,consent_id);

ALTER TABLE photo.consent_events ADD FOREIGN KEY (actor_id) REFERENCES photo.users(id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.consent_events (actor_id);

ALTER TABLE photo.workflow_consent_links ADD FOREIGN KEY (tenant_id,project_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.workflow_consent_links (tenant_id,project_id,workflow_id);

ALTER TABLE photo.workflow_consent_links ADD FOREIGN KEY (tenant_id,project_id,consent_id) REFERENCES photo.consent_grants(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.workflow_consent_links (tenant_id,project_id,consent_id);

ALTER TABLE photo.correction_events ADD FOREIGN KEY (tenant_id,project_id,session_id) REFERENCES photo.shoot_sessions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.correction_events (tenant_id,project_id,session_id);

ALTER TABLE photo.correction_events ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.correction_events (tenant_id,asset_version_id);

ALTER TABLE photo.correction_events ADD FOREIGN KEY (tenant_id,project_id,base_document_id) REFERENCES photo.v2_documents(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.correction_events (tenant_id,project_id,base_document_id);

ALTER TABLE photo.correction_events ADD FOREIGN KEY (tenant_id,project_id,approved_document_id) REFERENCES photo.v2_documents(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.correction_events (tenant_id,project_id,approved_document_id);

ALTER TABLE photo.correction_events ADD FOREIGN KEY (tenant_id,project_id,consent_id) REFERENCES photo.consent_grants(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.correction_events (tenant_id,project_id,consent_id);

ALTER TABLE photo.correction_events ADD FOREIGN KEY (approved_by) REFERENCES photo.users(id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.correction_events (approved_by);

ALTER TABLE photo.correction_events ADD FOREIGN KEY (tenant_id,project_id,person_id) REFERENCES photo.project_persons(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.correction_events (tenant_id,project_id,person_id);

ALTER TABLE photo.correction_events ADD FOREIGN KEY (tenant_id,project_id,scene_group_id) REFERENCES photo.scene_groups(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.correction_events (tenant_id,project_id,scene_group_id);

ALTER TABLE photo.learning_profiles ADD FOREIGN KEY (tenant_id,project_id,person_id) REFERENCES photo.project_persons(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_profiles (tenant_id,project_id,person_id);

ALTER TABLE photo.learning_profiles ADD FOREIGN KEY (created_by) REFERENCES photo.users(id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_profiles (created_by);

ALTER TABLE photo.learning_profiles ADD FOREIGN KEY (tenant_id,project_id,active_version_id) REFERENCES photo.learning_versions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_profiles (tenant_id,project_id,active_version_id);

ALTER TABLE photo.learning_versions ADD FOREIGN KEY (tenant_id,project_id,profile_id) REFERENCES photo.learning_profiles(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_versions (tenant_id,project_id,profile_id);

ALTER TABLE photo.learning_versions ADD FOREIGN KEY (tenant_id,project_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_versions (tenant_id,project_id,workflow_id);

ALTER TABLE photo.learning_versions ADD FOREIGN KEY (tenant_id,training_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_versions (tenant_id,training_artifact_id);

ALTER TABLE photo.learning_versions ADD FOREIGN KEY (tenant_id,model_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_versions (tenant_id,model_artifact_id);

ALTER TABLE photo.learning_versions ADD FOREIGN KEY (tenant_id,evaluation_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_versions (tenant_id,evaluation_artifact_id);

ALTER TABLE photo.learning_versions ADD FOREIGN KEY (tenant_id,project_id,parent_version_id) REFERENCES photo.learning_versions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_versions (tenant_id,project_id,parent_version_id);

ALTER TABLE photo.version_eligibility ADD FOREIGN KEY (tenant_id,project_id,version_id) REFERENCES photo.learning_versions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.version_eligibility (tenant_id,project_id,version_id);

ALTER TABLE photo.learning_example_links ADD FOREIGN KEY (tenant_id,project_id,version_id) REFERENCES photo.learning_versions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_example_links (tenant_id,project_id,version_id);

ALTER TABLE photo.learning_example_links ADD FOREIGN KEY (tenant_id,project_id,event_id) REFERENCES photo.correction_events(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_example_links (tenant_id,project_id,event_id);

ALTER TABLE photo.learning_example_links ADD FOREIGN KEY (tenant_id,project_id,consent_id) REFERENCES photo.consent_grants(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_example_links (tenant_id,project_id,consent_id);

ALTER TABLE photo.model_use_links ADD FOREIGN KEY (tenant_id,project_id,document_id) REFERENCES photo.v2_documents(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.model_use_links (tenant_id,project_id,document_id);

ALTER TABLE photo.model_use_links ADD FOREIGN KEY (tenant_id,project_id,version_id) REFERENCES photo.learning_versions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.model_use_links (tenant_id,project_id,version_id);

ALTER TABLE photo.profile_activations ADD FOREIGN KEY (tenant_id,project_id,profile_id) REFERENCES photo.learning_profiles(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.profile_activations (tenant_id,project_id,profile_id);

ALTER TABLE photo.profile_activations ADD FOREIGN KEY (tenant_id,project_id,version_id) REFERENCES photo.learning_versions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.profile_activations (tenant_id,project_id,version_id);

ALTER TABLE photo.profile_activations ADD FOREIGN KEY (actor_id) REFERENCES photo.users(id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.profile_activations (actor_id);

ALTER TABLE photo.profile_activations ADD FOREIGN KEY (tenant_id,project_id,previous_version_id) REFERENCES photo.learning_versions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.profile_activations (tenant_id,project_id,previous_version_id);

ALTER TABLE photo.consent_cleanup ADD FOREIGN KEY (tenant_id,project_id,consent_id) REFERENCES photo.consent_grants(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.consent_cleanup (tenant_id,project_id,consent_id);

ALTER TABLE photo.consent_cleanup ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.consent_cleanup (tenant_id,artifact_id);

ALTER TABLE photo.capture_devices ADD FOREIGN KEY (created_by) REFERENCES photo.users(id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.capture_devices (created_by);

ALTER TABLE photo.capture_challenges ADD FOREIGN KEY (tenant_id,project_id,device_id) REFERENCES photo.capture_devices(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.capture_challenges (tenant_id,project_id,device_id);

ALTER TABLE photo.capture_receipts ADD FOREIGN KEY (tenant_id,project_id,device_id) REFERENCES photo.capture_devices(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.capture_receipts (tenant_id,project_id,device_id);

ALTER TABLE photo.capture_receipts ADD FOREIGN KEY (tenant_id,project_id,session_id) REFERENCES photo.shoot_sessions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.capture_receipts (tenant_id,project_id,session_id);

ALTER TABLE photo.capture_receipts ADD FOREIGN KEY (tenant_id,upload_id) REFERENCES photo.upload_sessions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.capture_receipts (tenant_id,upload_id);

ALTER TABLE photo.capture_receipt_assets ADD FOREIGN KEY (tenant_id,project_id,receipt_id) REFERENCES photo.capture_receipts(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.capture_receipt_assets (tenant_id,project_id,receipt_id);

ALTER TABLE photo.capture_receipt_assets ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.capture_receipt_assets (tenant_id,asset_version_id);

ALTER TABLE photo.capture_alerts ADD FOREIGN KEY (tenant_id,project_id,session_id) REFERENCES photo.shoot_sessions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.capture_alerts (tenant_id,project_id,session_id);

ALTER TABLE photo.capture_alerts ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.capture_alerts (tenant_id,asset_version_id);

ALTER TABLE photo.capture_alerts ADD FOREIGN KEY (tenant_id,report_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.capture_alerts (tenant_id,report_artifact_id);

ALTER TABLE photo.capture_alerts ADD FOREIGN KEY (tenant_id,project_id,receipt_id) REFERENCES photo.capture_receipts(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.capture_alerts (tenant_id,project_id,receipt_id);

ALTER TABLE photo.capture_alerts ADD FOREIGN KEY (acknowledged_by) REFERENCES photo.users(id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.capture_alerts (acknowledged_by);

ALTER TABLE photo.shot_lists ADD FOREIGN KEY (tenant_id,project_id,session_id) REFERENCES photo.shoot_sessions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.shot_lists (tenant_id,project_id,session_id);

ALTER TABLE photo.shot_lists ADD FOREIGN KEY (tenant_id,project_id,document_id) REFERENCES photo.v2_documents(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.shot_lists (tenant_id,project_id,document_id);

ALTER TABLE photo.semantic_intents ADD FOREIGN KEY (tenant_id,project_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.semantic_intents (tenant_id,project_id,workflow_id);

ALTER TABLE photo.semantic_intents ADD FOREIGN KEY (tenant_id,reference_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.semantic_intents (tenant_id,reference_version_id);

ALTER TABLE photo.semantic_intents ADD FOREIGN KEY (tenant_id,snapshot_id) REFERENCES photo.snapshots(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.semantic_intents (tenant_id,snapshot_id);

ALTER TABLE photo.semantic_intents ADD FOREIGN KEY (tenant_id,intent_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.semantic_intents (tenant_id,intent_artifact_id);

ALTER TABLE photo.semantic_targets ADD FOREIGN KEY (tenant_id,project_id,intent_id) REFERENCES photo.semantic_intents(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.semantic_targets (tenant_id,project_id,intent_id);

ALTER TABLE photo.semantic_targets ADD FOREIGN KEY (tenant_id,child_run_id) REFERENCES photo.runs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.semantic_targets (tenant_id,child_run_id);

ALTER TABLE photo.semantic_targets ADD FOREIGN KEY (tenant_id,project_id,batch_id) REFERENCES photo.v2_review_batches(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.semantic_targets (tenant_id,project_id,batch_id);

ALTER TABLE photo.semantic_targets ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.semantic_targets (tenant_id,asset_version_id);

ALTER TABLE photo.semantic_targets ADD FOREIGN KEY (tenant_id,candidate_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.semantic_targets (tenant_id,candidate_artifact_id);

ALTER TABLE photo.semantic_targets ADD FOREIGN KEY (tenant_id,review_id) REFERENCES photo.reviews(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.semantic_targets (tenant_id,review_id);

ALTER TABLE photo.semantic_targets ADD FOREIGN KEY (tenant_id,override_id) REFERENCES photo.overrides(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.semantic_targets (tenant_id,override_id);

ALTER TABLE photo.delivery_packages ADD FOREIGN KEY (tenant_id,project_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.delivery_packages (tenant_id,project_id,workflow_id);

ALTER TABLE photo.delivery_packages ADD FOREIGN KEY (tenant_id,snapshot_id) REFERENCES photo.snapshots(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.delivery_packages (tenant_id,snapshot_id);

ALTER TABLE photo.delivery_packages ADD FOREIGN KEY (tenant_id,policy_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.delivery_packages (tenant_id,policy_artifact_id);

ALTER TABLE photo.delivery_packages ADD FOREIGN KEY (tenant_id,manifest_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.delivery_packages (tenant_id,manifest_artifact_id);

ALTER TABLE photo.delivery_items ADD FOREIGN KEY (tenant_id,project_id,package_id) REFERENCES photo.delivery_packages(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.delivery_items (tenant_id,project_id,package_id);

ALTER TABLE photo.delivery_items ADD FOREIGN KEY (tenant_id,preset_id) REFERENCES photo.export_presets(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.delivery_items (tenant_id,preset_id);

ALTER TABLE photo.delivery_items ADD FOREIGN KEY (tenant_id,export_id) REFERENCES photo.exports(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.delivery_items (tenant_id,export_id);

ALTER TABLE photo.interchange_jobs ADD FOREIGN KEY (tenant_id,project_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.interchange_jobs (tenant_id,project_id,workflow_id);

ALTER TABLE photo.interchange_jobs ADD FOREIGN KEY (tenant_id,mapping_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.interchange_jobs (tenant_id,mapping_artifact_id);

ALTER TABLE photo.interchange_jobs ADD FOREIGN KEY (tenant_id,report_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.interchange_jobs (tenant_id,report_artifact_id);

ALTER TABLE photo.interchange_jobs ADD FOREIGN KEY (tenant_id,bundle_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.interchange_jobs (tenant_id,bundle_artifact_id);

ALTER TABLE photo.provenance_manifests ADD FOREIGN KEY (tenant_id,export_id) REFERENCES photo.exports(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.provenance_manifests (tenant_id,export_id);

ALTER TABLE photo.provenance_manifests ADD FOREIGN KEY (tenant_id,project_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.provenance_manifests (tenant_id,project_id,workflow_id);

ALTER TABLE photo.provenance_manifests ADD FOREIGN KEY (tenant_id,manifest_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.provenance_manifests (tenant_id,manifest_artifact_id);

ALTER TABLE photo.provenance_manifests ADD FOREIGN KEY (tenant_id,signed_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.provenance_manifests (tenant_id,signed_artifact_id);

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

ALTER TABLE photo.learning_safety_reviews ADD FOREIGN KEY (tenant_id,project_id,version_id) REFERENCES photo.learning_versions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_safety_reviews (tenant_id,project_id,version_id);

ALTER TABLE photo.learning_safety_reviews ADD FOREIGN KEY (actor_id) REFERENCES photo.users(id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.learning_safety_reviews (actor_id);

ALTER TABLE photo.interchange_targets ADD FOREIGN KEY (tenant_id,project_id,job_id) REFERENCES photo.interchange_jobs(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.interchange_targets (tenant_id,project_id,job_id);

ALTER TABLE photo.interchange_targets ADD FOREIGN KEY (tenant_id,project_id,proposed_document_id) REFERENCES photo.v2_documents(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.interchange_targets (tenant_id,project_id,proposed_document_id);

ALTER TABLE photo.interchange_targets ADD FOREIGN KEY (tenant_id,project_id,batch_id) REFERENCES photo.v2_review_batches(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ON photo.interchange_targets (tenant_id,project_id,batch_id);

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

ALTER TABLE photo.shoot_sessions ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.shoot_sessions FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.shoot_sessions TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.shoot_sessions TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.shoot_sessions FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.shoot_sessions TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.shoot_sessions TO photo_fn_owner;

CREATE INDEX ON photo.shoot_sessions (tenant_id,project_id,created_at,id);

ALTER TABLE photo.session_analyses ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.session_analyses FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.session_analyses TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.session_analyses TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.session_analyses FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.session_analyses TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.session_analyses TO photo_fn_owner;

CREATE INDEX ON photo.session_analyses (tenant_id,project_id,created_at,id);

ALTER TABLE photo.scene_groups ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.scene_groups FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.scene_groups TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.scene_groups TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.scene_groups FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.scene_groups TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.scene_groups TO photo_fn_owner;

CREATE INDEX ON photo.scene_groups (tenant_id,project_id,created_at,id);

ALTER TABLE photo.scene_members ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.scene_members FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.scene_members TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.scene_members TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.scene_members FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.scene_members TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.scene_members TO photo_fn_owner;

CREATE INDEX ON photo.scene_members (tenant_id,project_id,created_at,id);

ALTER TABLE photo.project_persons ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.project_persons FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.project_persons TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.project_persons TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.project_persons FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.project_persons TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.project_persons TO photo_fn_owner;

CREATE INDEX ON photo.project_persons (tenant_id,project_id,created_at,id);

ALTER TABLE photo.person_observations ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.person_observations FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.person_observations TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.person_observations TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.person_observations FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.person_observations TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.person_observations TO photo_fn_owner;

CREATE INDEX ON photo.person_observations (tenant_id,project_id,created_at,id);

ALTER TABLE photo.consent_grants ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.consent_grants FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.consent_grants TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.consent_grants TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.consent_grants FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.consent_grants TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.consent_grants TO photo_fn_owner;

CREATE INDEX ON photo.consent_grants (tenant_id,project_id,created_at,id);

ALTER TABLE photo.consent_events ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.consent_events FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.consent_events TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.consent_events TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.consent_events FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.consent_events TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.consent_events TO photo_fn_owner;

CREATE INDEX ON photo.consent_events (tenant_id,project_id,created_at,id);

ALTER TABLE photo.workflow_consent_links ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.workflow_consent_links FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.workflow_consent_links TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.workflow_consent_links TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.workflow_consent_links FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.workflow_consent_links TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.workflow_consent_links TO photo_fn_owner;

CREATE INDEX ON photo.workflow_consent_links (tenant_id,project_id,created_at,id);

ALTER TABLE photo.correction_events ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.correction_events FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.correction_events TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.correction_events TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.correction_events FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.correction_events TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.correction_events TO photo_fn_owner;

CREATE INDEX ON photo.correction_events (tenant_id,project_id,created_at,id);

ALTER TABLE photo.learning_profiles ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.learning_profiles FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.learning_profiles TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.learning_profiles TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.learning_profiles FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.learning_profiles TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.learning_profiles TO photo_fn_owner;

CREATE INDEX ON photo.learning_profiles (tenant_id,project_id,created_at,id);

ALTER TABLE photo.learning_versions ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.learning_versions FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.learning_versions TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.learning_versions TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.learning_versions FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.learning_versions TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.learning_versions TO photo_fn_owner;

CREATE INDEX ON photo.learning_versions (tenant_id,project_id,created_at,id);

ALTER TABLE photo.version_eligibility ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.version_eligibility FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.version_eligibility TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.version_eligibility TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.version_eligibility FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.version_eligibility TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.version_eligibility TO photo_fn_owner;

CREATE INDEX ON photo.version_eligibility (tenant_id,project_id,created_at,id);

ALTER TABLE photo.learning_example_links ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.learning_example_links FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.learning_example_links TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.learning_example_links TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.learning_example_links FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.learning_example_links TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.learning_example_links TO photo_fn_owner;

CREATE INDEX ON photo.learning_example_links (tenant_id,project_id,created_at,id);

ALTER TABLE photo.model_use_links ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.model_use_links FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.model_use_links TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.model_use_links TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.model_use_links FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.model_use_links TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.model_use_links TO photo_fn_owner;

CREATE INDEX ON photo.model_use_links (tenant_id,project_id,created_at,id);

ALTER TABLE photo.profile_activations ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.profile_activations FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.profile_activations TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.profile_activations TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.profile_activations FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.profile_activations TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.profile_activations TO photo_fn_owner;

CREATE INDEX ON photo.profile_activations (tenant_id,project_id,created_at,id);

ALTER TABLE photo.consent_cleanup ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.consent_cleanup FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.consent_cleanup TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.consent_cleanup TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.consent_cleanup FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.consent_cleanup TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.consent_cleanup TO photo_fn_owner;

CREATE INDEX ON photo.consent_cleanup (tenant_id,project_id,created_at,id);

ALTER TABLE photo.capture_devices ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.capture_devices FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.capture_devices TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.capture_devices TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.capture_devices FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.capture_devices TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.capture_devices TO photo_fn_owner;

CREATE INDEX ON photo.capture_devices (tenant_id,project_id,created_at,id);

ALTER TABLE photo.capture_challenges ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.capture_challenges FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.capture_challenges TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.capture_challenges TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.capture_challenges FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.capture_challenges TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.capture_challenges TO photo_fn_owner;

CREATE INDEX ON photo.capture_challenges (tenant_id,project_id,created_at,id);

ALTER TABLE photo.capture_receipts ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.capture_receipts FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.capture_receipts TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.capture_receipts TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.capture_receipts FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.capture_receipts TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.capture_receipts TO photo_fn_owner;

CREATE INDEX ON photo.capture_receipts (tenant_id,project_id,created_at,id);

ALTER TABLE photo.capture_receipt_assets ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.capture_receipt_assets FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.capture_receipt_assets TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.capture_receipt_assets TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.capture_receipt_assets FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.capture_receipt_assets TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.capture_receipt_assets TO photo_fn_owner;

CREATE INDEX ON photo.capture_receipt_assets (tenant_id,project_id,created_at,id);

ALTER TABLE photo.capture_alerts ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.capture_alerts FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.capture_alerts TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.capture_alerts TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.capture_alerts FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.capture_alerts TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.capture_alerts TO photo_fn_owner;

CREATE INDEX ON photo.capture_alerts (tenant_id,project_id,created_at,id);

ALTER TABLE photo.shot_lists ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.shot_lists FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.shot_lists TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.shot_lists TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.shot_lists FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.shot_lists TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.shot_lists TO photo_fn_owner;

CREATE INDEX ON photo.shot_lists (tenant_id,project_id,created_at,id);

ALTER TABLE photo.semantic_intents ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.semantic_intents FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.semantic_intents TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.semantic_intents TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.semantic_intents FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.semantic_intents TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.semantic_intents TO photo_fn_owner;

CREATE INDEX ON photo.semantic_intents (tenant_id,project_id,created_at,id);

ALTER TABLE photo.semantic_targets ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.semantic_targets FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.semantic_targets TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.semantic_targets TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.semantic_targets FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.semantic_targets TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.semantic_targets TO photo_fn_owner;

CREATE INDEX ON photo.semantic_targets (tenant_id,project_id,created_at,id);

ALTER TABLE photo.delivery_packages ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.delivery_packages FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.delivery_packages TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.delivery_packages TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.delivery_packages FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.delivery_packages TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.delivery_packages TO photo_fn_owner;

CREATE INDEX ON photo.delivery_packages (tenant_id,project_id,created_at,id);

ALTER TABLE photo.delivery_items ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.delivery_items FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.delivery_items TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.delivery_items TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.delivery_items FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.delivery_items TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.delivery_items TO photo_fn_owner;

CREATE INDEX ON photo.delivery_items (tenant_id,project_id,created_at,id);

ALTER TABLE photo.interchange_jobs ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.interchange_jobs FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.interchange_jobs TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.interchange_jobs TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.interchange_jobs FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.interchange_jobs TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.interchange_jobs TO photo_fn_owner;

CREATE INDEX ON photo.interchange_jobs (tenant_id,project_id,created_at,id);

ALTER TABLE photo.provenance_manifests ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.provenance_manifests FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.provenance_manifests TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.provenance_manifests TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.provenance_manifests FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.provenance_manifests TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.provenance_manifests TO photo_fn_owner;

CREATE INDEX ON photo.provenance_manifests (tenant_id,project_id,created_at,id);

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

ALTER TABLE photo.learning_safety_reviews ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.learning_safety_reviews FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.learning_safety_reviews TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.learning_safety_reviews TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.learning_safety_reviews FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.learning_safety_reviews TO photo_api;

GRANT SELECT, INSERT, DELETE ON photo.learning_safety_reviews TO photo_fn_owner;

CREATE INDEX ON photo.learning_safety_reviews (tenant_id,project_id,created_at,id);

ALTER TABLE photo.interchange_targets ENABLE ROW LEVEL SECURITY;

ALTER TABLE photo.interchange_targets FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_scope ON photo.interchange_targets TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE POLICY trusted_functions ON photo.interchange_targets TO photo_fn_owner USING (true) WITH CHECK (true);

REVOKE ALL ON photo.interchange_targets FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;

GRANT SELECT ON photo.interchange_targets TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.interchange_targets TO photo_fn_owner;

CREATE INDEX ON photo.interchange_targets (tenant_id,project_id,created_at,id);

CREATE UNIQUE INDEX ON photo.consent_grants (tenant_id,project_id,purpose,person_id) WHERE person_id IS NOT NULL;

CREATE UNIQUE INDEX ON photo.consent_grants (tenant_id,project_id,purpose) WHERE person_id IS NULL;

CREATE UNIQUE INDEX ON photo.v2_document_streams (tenant_id,project_id,asset_version_id,stream_type) WHERE asset_version_id IS NOT NULL;
