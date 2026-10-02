-- V24 device identity and immutable capture receipt foundation.
-- +goose Up
CREATE TABLE photo.capture_devices (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 revision bigint NOT NULL DEFAULT 1 CHECK (revision>0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 label text NOT NULL CHECK (length(label) BETWEEN 1 AND 120),
 public_key text NOT NULL CHECK (public_key ~ '^[A-Za-z0-9_-]{43}$'),
 credential_epoch bigint NOT NULL DEFAULT 1 CHECK (credential_epoch>0),
 created_by uuid NOT NULL REFERENCES photo.users(id),
 state text NOT NULL DEFAULT 'active' CHECK (state IN ('active','revoked')),
 last_seen_at timestamptz,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,project_id,id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id)
  DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX capture_devices_active_key
 ON photo.capture_devices(tenant_id,project_id,public_key) WHERE state='active';
CREATE INDEX capture_devices_project_created
 ON photo.capture_devices(tenant_id,project_id,created_at,id);
ALTER TABLE photo.capture_devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.capture_devices FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.capture_devices TO photo_api
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.capture_devices TO photo_fn_owner
 USING(true) WITH CHECK(true);
REVOKE ALL ON photo.capture_devices FROM PUBLIC,photo_api,photo_worker,
 photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT ON photo.capture_devices TO photo_api;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.capture_devices TO photo_fn_owner;
CREATE TRIGGER capture_devices_revision BEFORE UPDATE ON photo.capture_devices
 FOR EACH ROW EXECUTE FUNCTION photo.bump_v2_revision();

CREATE TABLE photo.capture_challenges (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 revision bigint NOT NULL DEFAULT 1 CHECK (revision>0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 device_id uuid NOT NULL,
 nonce_ciphertext bytea NOT NULL,
 key_version text NOT NULL CHECK (length(key_version) BETWEEN 1 AND 256),
 nonce_hash text NOT NULL CHECK (nonce_hash ~ '^[0-9a-f]{64}$'),
 credential_epoch bigint NOT NULL CHECK (credential_epoch>0),
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,project_id,id),
 UNIQUE(tenant_id,nonce_hash),
 FOREIGN KEY(tenant_id,project_id,device_id)
  REFERENCES photo.capture_devices(tenant_id,project_id,id)
  DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX capture_challenges_device_expiry
 ON photo.capture_challenges(tenant_id,device_id,expires_at);
ALTER TABLE photo.capture_challenges ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.capture_challenges FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.capture_challenges TO photo_fn_owner
 USING(true) WITH CHECK(true);
REVOKE ALL ON photo.capture_challenges FROM PUBLIC,photo_api,photo_worker,
 photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.capture_challenges TO photo_fn_owner;
CREATE TRIGGER capture_challenges_revision BEFORE UPDATE ON photo.capture_challenges
 FOR EACH ROW EXECUTE FUNCTION photo.bump_v2_revision();

CREATE TABLE photo.capture_receipts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 device_id uuid NOT NULL,
 session_id uuid NOT NULL,
 sequence bigint NOT NULL CHECK (sequence>=0),
 content_sha text NOT NULL CHECK (content_sha ~ '^[0-9a-f]{64}$'),
 upload_id uuid NOT NULL,
 captured_at timestamptz,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,project_id,id),
 UNIQUE(tenant_id,device_id,sequence),
 FOREIGN KEY(tenant_id,project_id,device_id)
  REFERENCES photo.capture_devices(tenant_id,project_id,id)
  DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,project_id,session_id)
  REFERENCES photo.shoot_sessions(tenant_id,project_id,id)
  DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,upload_id)
  REFERENCES photo.upload_sessions(tenant_id,id)
  DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX capture_receipts_session_created
 ON photo.capture_receipts(tenant_id,project_id,session_id,created_at,id);
ALTER TABLE photo.capture_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.capture_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.capture_receipts TO photo_api
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.capture_receipts TO photo_fn_owner
 USING(true) WITH CHECK(true);
REVOKE ALL ON photo.capture_receipts FROM PUBLIC,photo_api,photo_worker,
 photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT ON photo.capture_receipts TO photo_api;
GRANT SELECT,INSERT,DELETE ON photo.capture_receipts TO photo_fn_owner;
CREATE TRIGGER capture_receipts_immutable BEFORE UPDATE ON photo.capture_receipts
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();

CREATE TABLE photo.capture_receipt_assets (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 receipt_id uuid NOT NULL,
 asset_version_id uuid NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,project_id,id),
 UNIQUE(tenant_id,receipt_id),
 FOREIGN KEY(tenant_id,project_id,receipt_id)
  REFERENCES photo.capture_receipts(tenant_id,project_id,id)
  DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,asset_version_id)
  REFERENCES photo.asset_versions(tenant_id,id)
  DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE photo.capture_receipt_assets ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.capture_receipt_assets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.capture_receipt_assets TO photo_api
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.capture_receipt_assets TO photo_fn_owner
 USING(true) WITH CHECK(true);
REVOKE ALL ON photo.capture_receipt_assets FROM PUBLIC,photo_api,photo_worker,
 photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT ON photo.capture_receipt_assets TO photo_api;
GRANT SELECT,INSERT,DELETE ON photo.capture_receipt_assets TO photo_fn_owner;
CREATE TRIGGER capture_receipt_assets_immutable BEFORE UPDATE ON photo.capture_receipt_assets
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();

-- Mutations verify the authenticated user session again inside the
-- SECURITY DEFINER boundary. Generic API roles have no table DML.
-- +goose StatementBegin
CREATE FUNCTION photo.register_capture_device(p_tenant uuid,p_project uuid,
 p_label text,p_key text,p_actor uuid,p_session uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE created photo.capture_devices%ROWTYPE; decoded bytea;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR p_session IS NULL
  OR length(coalesce(p_label,'')) NOT BETWEEN 1 AND 120 OR
  coalesce(p_key,'') !~ '^[A-Za-z0-9_-]{43}$' THEN
  RAISE EXCEPTION 'V24_DEVICE_INVALID' USING ERRCODE='22023'; END IF;
 decoded:=decode(translate(p_key,'-_','+/')||'=','base64');
 IF octet_length(decoded)<>32 OR
  rtrim(translate(encode(decoded,'base64'),'+/','-_'),'=')<>p_key THEN
  RAISE EXCEPTION 'V24_DEVICE_INVALID' USING ERRCODE='22023'; END IF;
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) a
  JOIN photo.projects p ON p.tenant_id=p_tenant AND p.id=p_project
  WHERE a.role IN('owner','admin') AND p.archived_at IS NULL) THEN
  RAISE EXCEPTION 'V24_DEVICE_FORBIDDEN' USING ERRCODE='42501'; END IF;
 INSERT INTO photo.capture_devices(tenant_id,project_id,label,public_key,created_by)
 VALUES(p_tenant,p_project,p_label,p_key,p_actor) RETURNING * INTO created;
 RETURN jsonb_build_object('id',created.id,'revision',created.revision::text,
  'project_id',created.project_id,'label',created.label,'public_key',created.public_key,
  'credential_epoch',created.credential_epoch::text,'state',created.state,
  'created_at',created.created_at);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_capture_device(uuid,uuid,text,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_capture_device(uuid,uuid,text,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_capture_device(uuid,uuid,text,text,uuid,uuid) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.revoke_capture_device(p_tenant uuid,p_id uuid,p_expected bigint,
 p_actor uuid,p_session uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.capture_devices%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_id IS NULL OR p_expected<1 OR p_actor IS NULL OR p_session IS NULL THEN
  RAISE EXCEPTION 'V24_DEVICE_INVALID' USING ERRCODE='22023'; END IF;
 SELECT * INTO d FROM photo.capture_devices WHERE tenant_id=p_tenant AND id=p_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V24_DEVICE_NOT_FOUND' USING ERRCODE='P0002'; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) a
  JOIN photo.projects p ON p.tenant_id=p_tenant AND p.id=d.project_id
  WHERE a.role IN('owner','admin') AND p.archived_at IS NULL) THEN
  RAISE EXCEPTION 'V24_DEVICE_FORBIDDEN' USING ERRCODE='42501'; END IF;
 IF d.revision<>p_expected OR d.state<>'active' THEN
  RAISE EXCEPTION 'V24_DEVICE_REVISION_CONFLICT' USING ERRCODE='40001'; END IF;
 UPDATE photo.capture_devices SET state='revoked',credential_epoch=credential_epoch+1
  WHERE tenant_id=p_tenant AND id=p_id RETURNING * INTO d;
 RETURN jsonb_build_object('id',d.id,'revision',d.revision::text,
  'project_id',d.project_id,'label',d.label,'public_key',d.public_key,
  'credential_epoch',d.credential_epoch::text,'state',d.state,
  'created_at',d.created_at);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.revoke_capture_device(uuid,uuid,bigint,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.revoke_capture_device(uuid,uuid,bigint,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.revoke_capture_device(uuid,uuid,bigint,uuid,uuid) TO photo_api;

-- Extend the approved tenant metadata purge allowlist after storage cleanup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finish_tenant_deletion(p_job uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE;t text;
 tables_to_purge constant text[]:=ARRAY[
  'capture_receipt_assets','capture_receipts','capture_challenges','capture_devices','learning_safety_reviews','model_use_links','learning_example_links','profile_activations','version_eligibility','consent_cleanup','learning_versions','correction_events','learning_profiles','v22_analysis_decisions',
  'workflow_consent_links','consent_events','person_observations','scene_members','scene_groups','session_analyses','consent_grants','project_persons','shoot_sessions',
  'v2_document_approvals','v2_document_refs','v2_task_inputs','v2_review_batches','v2_artifact_links','model_qualifications','v2_documents','v2_document_streams','v2_workflows','v2_draft_uploads',
  'apply_v4_stages','apply_nodes','apply_builds','artifact_leases','asset_embeddings','asset_version_sources','bulk_item_results','collection_items',
  'dead_letters','defect_branches','derivatives','download_archive_plans','download_export_plans','download_intents','download_job_items','download_volumes','editor_drafts','export_batch_children','graph_expansions','ingest_catalog_items','ingest_decode_items','ingest_node_plans','ingest_originals','ingest_progress','ingest_rejections','interactive_tasks','media_retry_nodes','output_uploads','project_acl','publications','resource_reservations','resource_slots','result_cache','reviews','run_events','sections','share_sessions','snapshot_items','staging_cleanup','style_analysis_nodes','style_asset_bindings','style_candidates','style_import_builds','task_attempts','task_edges','task_results','training_pairs','upload_abort_cleanup','upload_parts','asset_versions','assets','backend_documents','bulk_operations','collections','download_build_state','download_jobs','export_batches','exports','ingest_upload_bindings','legacy_imports','media_retry_jobs','overrides','quota_reservations','share_links','snapshots','style_analysis_builds','style_versions','tasks','training_sets','upload_sessions','tenant_static_resources','artifacts','runs','styles','projects','export_presets','idempotency_records','inbox','invitations','outbox','scheduler_tenants','tenant_event_cursors','tenant_usage','worker_registrations'
 ];
BEGIN
 SELECT * INTO j FROM photo.deletion_jobs WHERE id=p_job AND resource_type='tenant' AND status='running' AND cleanup_token=p_token AND cleanup_lease_until>clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.tenants x WHERE x.id=j.tenant_id AND x.status='deleting') OR EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=j.tenant_id AND r.status IN('draft','queued','running','waiting_review','cancelling')) THEN RETURN false;END IF;
 SET CONSTRAINTS ALL DEFERRED;
 UPDATE photo.v2_document_streams SET current_document_id=NULL,approved_document_id=NULL WHERE tenant_id=j.tenant_id;
 FOREACH t IN ARRAY tables_to_purge LOOP EXECUTE format('DELETE FROM photo.%I WHERE tenant_id=$1',t) USING j.tenant_id;END LOOP;
 DELETE FROM photo.audit_log WHERE tenant_id=j.tenant_id AND action NOT LIKE 'tenant.%';UPDATE photo.memberships SET status='suspended',revision=revision+1 WHERE tenant_id=j.tenant_id AND status<>'suspended';
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES(j.tenant_id,'system',NULL,'tenant.deleted','tenant',j.tenant_id,gen_random_uuid(),jsonb_build_object('job_id',j.id,'cleanup_passes',j.cleanup_passes+1));
 UPDATE photo.tenants SET status='deleted',revision=revision+1 WHERE id=j.tenant_id AND status='deleting';UPDATE photo.deletion_jobs SET status='completed',cleanup_token=NULL,cleanup_lease_until=NULL,cleanup_passes=cleanup_passes+1,cursor=NULL,error=NULL WHERE id=j.id;SET CONSTRAINTS ALL IMMEDIATE;RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_tenant_deletion(uuid,uuid) OWNER TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
