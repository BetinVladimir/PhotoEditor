-- Explicit domain constraints and row security grants.

-- +goose Up

ALTER TABLE photo.sessions ADD FOREIGN KEY (user_id) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.sessions ADD FOREIGN KEY (parent_id) REFERENCES photo.sessions (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.mail_payloads ADD FOREIGN KEY (challenge_id) REFERENCES photo.auth_challenges (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.mail_payloads ADD FOREIGN KEY (invitation_id) REFERENCES photo.invitations (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.tenants ADD FOREIGN KEY (owner_user_id) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.memberships ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.memberships ADD FOREIGN KEY (user_id) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.invitations ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.invitations ADD FOREIGN KEY (invited_by) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.tenant_usage ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.audit_log ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.projects ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.project_acl ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.project_acl ADD FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.project_acl ADD FOREIGN KEY (user_id) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.artifacts ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.artifacts ADD FOREIGN KEY (tenant_id,created_by_task_id) REFERENCES photo.tasks (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.assets ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.assets ADD FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.assets ADD FOREIGN KEY (tenant_id,original_artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.asset_versions ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.asset_versions ADD FOREIGN KEY (tenant_id,asset_id) REFERENCES photo.assets (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.asset_versions ADD FOREIGN KEY (tenant_id,master_artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.asset_versions ADD FOREIGN KEY (tenant_id,parent_asset_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.asset_versions ADD FOREIGN KEY (tenant_id,created_by_run_id) REFERENCES photo.runs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.asset_version_sources ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.asset_version_sources ADD FOREIGN KEY (tenant_id,version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.asset_version_sources ADD FOREIGN KEY (tenant_id,source_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.collections ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.collections ADD FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.collections ADD FOREIGN KEY (tenant_id,source_collection_id) REFERENCES photo.collections (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.collections ADD FOREIGN KEY (tenant_id,source_run_id) REFERENCES photo.runs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.collections ADD FOREIGN KEY (tenant_id,applied_style_version_id) REFERENCES photo.style_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.sections ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.sections ADD FOREIGN KEY (tenant_id,collection_id) REFERENCES photo.collections (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.collection_items ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.collection_items ADD FOREIGN KEY (tenant_id,collection_id) REFERENCES photo.collections (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.collection_items ADD FOREIGN KEY (tenant_id,asset_id) REFERENCES photo.assets (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.collection_items ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.collection_items ADD FOREIGN KEY (tenant_id,created_by_run_id) REFERENCES photo.runs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.publications ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.publications ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.publications ADD FOREIGN KEY (published_by) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.share_links ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.share_links ADD FOREIGN KEY (tenant_id,collection_id) REFERENCES photo.collections (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.share_links ADD FOREIGN KEY (created_by) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.share_sessions ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.share_sessions ADD FOREIGN KEY (tenant_id,share_id) REFERENCES photo.share_links (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.share_sessions ADD FOREIGN KEY (tenant_id,collection_id) REFERENCES photo.collections (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.upload_sessions ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.upload_sessions ADD FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.upload_sessions ADD FOREIGN KEY (tenant_id,collection_id) REFERENCES photo.collections (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.upload_sessions ADD FOREIGN KEY (tenant_id,share_id) REFERENCES photo.share_links (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.upload_sessions ADD FOREIGN KEY (tenant_id,staging_artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.upload_sessions ADD FOREIGN KEY (tenant_id,verified_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.upload_sessions ADD FOREIGN KEY (tenant_id,reservation_id) REFERENCES photo.quota_reservations (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.upload_parts ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.upload_parts ADD FOREIGN KEY (tenant_id,upload_session_id) REFERENCES photo.upload_sessions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.quota_reservations ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.quota_reservations ADD FOREIGN KEY (tenant_id,share_id) REFERENCES photo.share_links (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.styles ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.styles ADD FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.styles ADD FOREIGN KEY (created_by) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.style_versions ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.style_versions ADD FOREIGN KEY (tenant_id,style_id) REFERENCES photo.styles (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.style_versions ADD FOREIGN KEY (tenant_id,training_run_id) REFERENCES photo.runs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.style_versions ADD FOREIGN KEY (tenant_id,export_artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.style_asset_bindings ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.style_asset_bindings ADD FOREIGN KEY (tenant_id,style_version_id) REFERENCES photo.style_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.style_asset_bindings ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.training_sets ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.training_sets ADD FOREIGN KEY (tenant_id,style_id) REFERENCES photo.styles (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.training_pairs ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.training_pairs ADD FOREIGN KEY (tenant_id,training_set_id) REFERENCES photo.training_sets (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.training_pairs ADD FOREIGN KEY (tenant_id,before_asset_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.training_pairs ADD FOREIGN KEY (tenant_id,after_asset_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.training_pairs ADD FOREIGN KEY (tenant_id,ignore_mask_artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.snapshots ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.snapshots ADD FOREIGN KEY (tenant_id,collection_id) REFERENCES photo.collections (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.snapshots ADD FOREIGN KEY (tenant_id,training_set_id) REFERENCES photo.training_sets (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.snapshots ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.snapshot_items ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.snapshot_items ADD FOREIGN KEY (tenant_id,snapshot_id) REFERENCES photo.snapshots (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.snapshot_items ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.snapshot_items ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.runs ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.runs ADD FOREIGN KEY (tenant_id,input_snapshot_id) REFERENCES photo.snapshots (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.runs ADD FOREIGN KEY (tenant_id,output_collection_id) REFERENCES photo.collections (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.runs ADD FOREIGN KEY (tenant_id,style_version_id) REFERENCES photo.style_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.runs ADD FOREIGN KEY (created_by) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.tasks ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.tasks ADD FOREIGN KEY (tenant_id,run_id) REFERENCES photo.runs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.tasks ADD FOREIGN KEY (tenant_id,params_artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.tasks ADD FOREIGN KEY (tenant_id,canonical_result_id) REFERENCES photo.task_results (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.task_edges ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.task_edges ADD FOREIGN KEY (tenant_id,run_id) REFERENCES photo.runs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.task_attempts ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.task_attempts ADD FOREIGN KEY (tenant_id,task_id) REFERENCES photo.tasks (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.task_results ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.task_results ADD FOREIGN KEY (tenant_id,task_id) REFERENCES photo.tasks (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.outbox ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.inbox ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.tenant_event_cursors ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.run_events ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.run_events ADD FOREIGN KEY (tenant_id,run_id) REFERENCES photo.runs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.run_events ADD FOREIGN KEY (tenant_id,collection_id) REFERENCES photo.collections (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.worker_registrations ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.result_cache ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.result_cache ADD FOREIGN KEY (tenant_id,result_id) REFERENCES photo.task_results (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.reviews ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.reviews ADD FOREIGN KEY (tenant_id,run_id) REFERENCES photo.runs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.reviews ADD FOREIGN KEY (tenant_id,task_id) REFERENCES photo.tasks (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.reviews ADD FOREIGN KEY (tenant_id,candidate_artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.reviews ADD FOREIGN KEY (tenant_id,base_artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.reviews ADD FOREIGN KEY (decided_by) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.overrides ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.overrides ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.overrides ADD FOREIGN KEY (created_by) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.editor_drafts ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.editor_drafts ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.editor_drafts ADD FOREIGN KEY (user_id) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.editor_drafts ADD FOREIGN KEY (tenant_id,last_preview_run_id) REFERENCES photo.runs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.bulk_operations ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.bulk_operations ADD FOREIGN KEY (tenant_id,collection_id) REFERENCES photo.collections (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.bulk_operations ADD FOREIGN KEY (tenant_id,snapshot_id) REFERENCES photo.snapshots (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.bulk_operations ADD FOREIGN KEY (created_by) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.bulk_operations ADD FOREIGN KEY (tenant_id,run_id) REFERENCES photo.runs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.bulk_item_results ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.bulk_item_results ADD FOREIGN KEY (tenant_id,bulk_operation_id) REFERENCES photo.bulk_operations (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.derivatives ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.derivatives ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.derivatives ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.derivatives ADD FOREIGN KEY (tenant_id,task_id) REFERENCES photo.tasks (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.export_presets ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.exports ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.exports ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.exports ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.exports ADD FOREIGN KEY (tenant_id,task_id) REFERENCES photo.tasks (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_jobs ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_jobs ADD FOREIGN KEY (tenant_id,collection_id) REFERENCES photo.collections (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_jobs ADD FOREIGN KEY (tenant_id,share_id) REFERENCES photo.share_links (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_jobs ADD FOREIGN KEY (tenant_id,snapshot_id) REFERENCES photo.snapshots (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_jobs ADD FOREIGN KEY (tenant_id,run_id) REFERENCES photo.runs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_job_items ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_job_items ADD FOREIGN KEY (tenant_id,download_job_id) REFERENCES photo.download_jobs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_job_items ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_job_items ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_volumes ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_volumes ADD FOREIGN KEY (tenant_id,download_job_id) REFERENCES photo.download_jobs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_volumes ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_volumes ADD FOREIGN KEY (tenant_id,manifest_artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_intents ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_intents ADD FOREIGN KEY (tenant_id,share_id) REFERENCES photo.share_links (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.download_intents ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.artifact_leases ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.artifact_leases ADD FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.idempotency_records ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.deletion_jobs ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.deletion_jobs ADD FOREIGN KEY (requested_by) REFERENCES photo.users (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.legacy_imports ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.legacy_imports ADD FOREIGN KEY (tenant_id,asset_version_id) REFERENCES photo.asset_versions (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.auth_mail_outbox ADD FOREIGN KEY (payload_id) REFERENCES photo.mail_payloads (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.scheduler_tenants ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.graph_expansions ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.graph_expansions ADD FOREIGN KEY (tenant_id,run_id) REFERENCES photo.runs (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.graph_expansions ADD FOREIGN KEY (tenant_id,parent_task_id) REFERENCES photo.tasks (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.resource_slots ADD FOREIGN KEY (tenant_id,task_id) REFERENCES photo.tasks (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.resource_reservations ADD FOREIGN KEY (tenant_id) REFERENCES photo.tenants (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.resource_reservations ADD FOREIGN KEY (tenant_id,task_id) REFERENCES photo.tasks (tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.asset_versions ADD UNIQUE (tenant_id,asset_id,id);

ALTER TABLE photo.collection_items ADD FOREIGN KEY (tenant_id,asset_id,asset_version_id) REFERENCES photo.asset_versions (tenant_id,asset_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.collection_items ADD FOREIGN KEY (tenant_id,collection_id,section_id) REFERENCES photo.sections (tenant_id,collection_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.collection_items ADD UNIQUE (tenant_id,id,asset_version_id);

ALTER TABLE photo.collection_items ADD UNIQUE (tenant_id,collection_id,id);

ALTER TABLE photo.publications ADD FOREIGN KEY (tenant_id,item_id,asset_version_id) REFERENCES photo.collection_items (tenant_id,id,asset_version_id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.collections ADD FOREIGN KEY (tenant_id,id,cover_item_id) REFERENCES photo.collection_items (tenant_id,collection_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.share_links ADD UNIQUE (tenant_id,id,collection_id);

ALTER TABLE photo.share_sessions ADD FOREIGN KEY (tenant_id,share_id,collection_id) REFERENCES photo.share_links (tenant_id,id,collection_id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.tasks ADD UNIQUE (tenant_id,run_id,id);

ALTER TABLE photo.task_edges ADD FOREIGN KEY (tenant_id,run_id,prerequisite_id) REFERENCES photo.tasks (tenant_id,run_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.task_edges ADD FOREIGN KEY (tenant_id,run_id,dependent_id) REFERENCES photo.tasks (tenant_id,run_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.task_results ADD FOREIGN KEY (tenant_id,task_id,attempt) REFERENCES photo.task_attempts (tenant_id,task_id,attempt) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.graph_expansions ADD FOREIGN KEY (tenant_id,run_id,parent_task_id) REFERENCES photo.tasks (tenant_id,run_id,id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE photo.users ADD CONSTRAINT users_domain_0 CHECK (locale IN ('ru','bg','en'));

ALTER TABLE photo.users ADD CONSTRAINT users_domain_1 CHECK (theme IN ('light','dark','system'));

ALTER TABLE photo.users ADD CONSTRAINT users_domain_2 CHECK (status IN ('active','disabled'));

ALTER TABLE photo.auth_challenges ADD CONSTRAINT auth_challenges_domain_0 CHECK (attempts BETWEEN 0 AND 5);

ALTER TABLE photo.sessions ADD CONSTRAINT sessions_domain_0 CHECK (client_type IN ('web','native'));

ALTER TABLE photo.mail_payloads ADD CONSTRAINT mail_payloads_domain_0 CHECK ((kind='otp' AND challenge_id IS NOT NULL AND invitation_id IS NULL) OR (kind='invite' AND invitation_id IS NOT NULL AND challenge_id IS NULL));

ALTER TABLE photo.memberships ADD CONSTRAINT memberships_domain_0 CHECK (role IN ('owner','admin','editor','viewer'));

ALTER TABLE photo.memberships ADD CONSTRAINT memberships_domain_1 CHECK (status IN ('active','suspended'));

ALTER TABLE photo.invitations ADD CONSTRAINT invitations_domain_0 CHECK (role IN ('admin','editor','viewer'));

ALTER TABLE photo.projects ADD CONSTRAINT projects_domain_0 CHECK (visibility IN ('workspace','restricted'));

ALTER TABLE photo.projects ADD CONSTRAINT projects_domain_1 CHECK (length(btrim(name)) BETWEEN 1 AND 120);

ALTER TABLE photo.project_acl ADD CONSTRAINT project_acl_domain_0 CHECK (role IN ('viewer','editor'));

ALTER TABLE photo.collections ADD CONSTRAINT collections_domain_0 CHECK (state IN ('draft','published','archived','deleting'));

ALTER TABLE photo.collections ADD CONSTRAINT collections_domain_1 CHECK (length(btrim(name)) BETWEEN 1 AND 120);

ALTER TABLE photo.collection_items ADD CONSTRAINT collection_items_domain_0 CHECK (rating BETWEEN 0 AND 5);

ALTER TABLE photo.collection_items ADD CONSTRAINT collection_items_domain_1 CHECK (visibility IN ('hidden','visible'));

ALTER TABLE photo.collection_items ADD CONSTRAINT collection_items_domain_2 CHECK (moderation_state IN ('pending_review','accepted','rejected'));

ALTER TABLE photo.collection_items ADD CONSTRAINT collection_items_domain_3 CHECK (jsonb_typeof(tags)='array');

ALTER TABLE photo.publications ADD CONSTRAINT publications_domain_0 CHECK (state IN ('published','revoked'));

ALTER TABLE photo.share_links ADD CONSTRAINT share_links_domain_0 CHECK (moderation IN ('owner_review','auto_after_validation'));

ALTER TABLE photo.share_links ADD CONSTRAINT share_links_domain_1 CHECK (jsonb_typeof(permissions)='object' AND permissions ?& ARRAY['view','download','upload']);

ALTER TABLE photo.share_links ADD CONSTRAINT share_links_domain_2 CHECK (jsonb_typeof(permissions->'view')='boolean' AND jsonb_typeof(permissions->'download')='boolean' AND jsonb_typeof(permissions->'upload')='boolean');

ALTER TABLE photo.upload_sessions ADD CONSTRAINT upload_sessions_domain_0 CHECK (expected_size>0);

ALTER TABLE photo.upload_sessions ADD CONSTRAINT upload_sessions_domain_1 CHECK (actual_size IS NULL OR actual_size BETWEEN 1 AND expected_size);

ALTER TABLE photo.upload_sessions ADD CONSTRAINT upload_sessions_domain_2 CHECK (state IN ('initializing','uploading','completing','verifying','verified','rejected','aborted','expired'));

ALTER TABLE photo.upload_parts ADD CONSTRAINT upload_parts_domain_0 CHECK (part_no BETWEEN 1 AND 10000);

ALTER TABLE photo.styles ADD CONSTRAINT styles_domain_0 CHECK (length(btrim(name)) BETWEEN 1 AND 120);

ALTER TABLE photo.styles ADD CONSTRAINT styles_domain_1 CHECK (length(description)<=2000);

ALTER TABLE photo.style_versions ADD CONSTRAINT style_versions_domain_0 CHECK (schema_version='2.0.0');

ALTER TABLE photo.style_versions ADD CONSTRAINT style_versions_domain_1 CHECK (renderer_version='photo_renderer_v2');

ALTER TABLE photo.style_versions ADD CONSTRAINT style_versions_domain_2 CHECK (jsonb_typeof(config_json)='object');

ALTER TABLE photo.style_versions ADD CONSTRAINT style_versions_domain_3 CHECK (octet_length(config_json::text)<=110000000);

ALTER TABLE photo.style_versions ADD CONSTRAINT style_versions_domain_4 CHECK (approval_status IN ('accepted','needs_review'));

ALTER TABLE photo.training_pairs ADD CONSTRAINT training_pairs_domain_0 CHECK (before_asset_version_id<>after_asset_version_id);

ALTER TABLE photo.snapshots ADD CONSTRAINT snapshots_domain_0 CHECK (kind IN ('input','training','selection','download'));

ALTER TABLE photo.runs ADD CONSTRAINT runs_domain_0 CHECK (kind IN ('analyze','apply','cull','preview','export','ingest','download','bulk','maintenance'));

ALTER TABLE photo.runs ADD CONSTRAINT runs_domain_1 CHECK (status IN ('draft','queued','running','waiting_review','cancelling','cancelled','completed','completed_with_errors','failed'));

ALTER TABLE photo.tasks ADD CONSTRAINT tasks_domain_0 CHECK (state IN ('blocked','ready','queued','running','succeeded','retry_wait','failed','cancelled','skipped'));

ALTER TABLE photo.tasks ADD CONSTRAINT tasks_domain_1 CHECK (resource_class IN ('cpu','gpu','media','archive','collection','mail'));

ALTER TABLE photo.tasks ADD CONSTRAINT tasks_domain_2 CHECK (attempt>=0 AND retry_count BETWEEN 0 AND 3 AND oom_count BETWEEN 0 AND 2);

ALTER TABLE photo.tasks ADD CONSTRAINT tasks_domain_3 CHECK ((state='running') = (lease_until IS NOT NULL));

ALTER TABLE photo.tasks ADD CONSTRAINT tasks_domain_4 CHECK (state<>'succeeded' OR canonical_result_id IS NOT NULL);

ALTER TABLE photo.task_edges ADD CONSTRAINT task_edges_domain_0 CHECK (prerequisite_id<>dependent_id);

ALTER TABLE photo.task_edges ADD CONSTRAINT task_edges_domain_1 CHECK (condition IN ('success','accepted','rejected','terminal'));

ALTER TABLE photo.reviews ADD CONSTRAINT reviews_domain_0 CHECK (state IN ('pending','decided'));

ALTER TABLE photo.reviews ADD CONSTRAINT reviews_domain_1 CHECK (decision IS NULL OR decision IN ('accept','reject','skip'));

ALTER TABLE photo.reviews ADD CONSTRAINT reviews_domain_2 CHECK ((state='pending' AND decision IS NULL) OR (state='decided' AND decision IS NOT NULL AND decided_by IS NOT NULL AND decided_at IS NOT NULL));

ALTER TABLE photo.download_jobs ADD CONSTRAINT download_jobs_domain_0 CHECK (status IN ('queued','preparing','building','uploading','ready','expired','failed','cancelling','cancelled'));

ALTER TABLE photo.resource_slots ADD CONSTRAINT resource_slots_domain_0 CHECK ((tenant_id IS NULL)=(task_id IS NULL));

CREATE UNIQUE INDEX one_owner_per_tenant ON photo.memberships(tenant_id) WHERE role='owner' AND status='active';

CREATE INDEX ON photo.collection_items(tenant_id,collection_id,position,id);

CREATE INDEX ON photo.outbox(created_at) WHERE sent_at IS NULL;

CREATE INDEX ON photo.tasks(tenant_id,state,next_attempt_at,id);

CREATE INDEX ON photo.artifacts(tenant_id,expires_at);

CREATE INDEX ON photo.artifact_leases(tenant_id,artifact_id,expires_at);

CREATE INDEX ON photo.run_events(tenant_id,collection_id,cursor);

CREATE INDEX ON photo.run_events(tenant_id,run_id,cursor);

CREATE INDEX ON photo.auth_challenges(email_normalized,created_at);

CREATE INDEX ON photo.sessions(family_id);

CREATE INDEX ON photo.upload_sessions(state,expires_at);

CREATE INDEX ON photo.download_jobs(tenant_id,status,expires_at);

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA photo TO photo_fn_owner;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.memberships TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.invitations TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.tenant_usage TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.audit_log TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.projects TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.project_acl TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.artifacts TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.assets TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.asset_versions TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.asset_version_sources TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.collections TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.sections TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.collection_items TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.publications TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.share_links TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.share_sessions TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.upload_sessions TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.upload_parts TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.quota_reservations TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.styles TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.style_versions TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.style_asset_bindings TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.training_sets TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.training_pairs TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.snapshots TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.snapshot_items TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.runs TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.tasks TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.task_edges TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.tenant_event_cursors TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.run_events TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.result_cache TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.reviews TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.overrides TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.editor_drafts TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.bulk_operations TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.bulk_item_results TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.derivatives TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.export_presets TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.exports TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.download_jobs TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.download_job_items TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.download_volumes TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.download_intents TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.artifact_leases TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.idempotency_records TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.deletion_jobs TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.legacy_imports TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.graph_expansions TO photo_api;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.users TO photo_auth;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.auth_challenges TO photo_auth;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.sessions TO photo_auth;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.auth_rate_limits TO photo_auth;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.auth_idempotency_records TO photo_auth;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.mail_payloads TO photo_auth;

GRANT SELECT, INSERT, UPDATE, DELETE ON photo.auth_mail_outbox TO photo_auth;

REVOKE ALL ON ALL FUNCTIONS IN SCHEMA photo FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.reject_content_update() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
DECLARE col text; BEGIN FOREACH col IN ARRAY TG_ARGV LOOP IF to_jsonb(NEW)->col IS DISTINCT FROM to_jsonb(OLD)->col THEN RAISE EXCEPTION 'immutable field %.%',TG_TABLE_NAME,col USING ERRCODE='23514'; END IF; END LOOP; RETURN NEW; END $$;
-- +goose StatementEnd

CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.asset_versions FOR EACH ROW EXECUTE FUNCTION photo.reject_content_update('tenant_id','id','asset_id','master_artifact_id','parent_asset_version_id','created_by_run_id','content_hash','version_no');

CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.collection_items FOR EACH ROW EXECUTE FUNCTION photo.reject_content_update('tenant_id','id','asset_id','asset_version_id');

CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.style_versions FOR EACH ROW EXECUTE FUNCTION photo.reject_content_update('tenant_id','id','style_id','version_no','config_json','config_hash','schema_version','renderer_version','training_run_id','metrics','approval_status');

CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.snapshots FOR EACH ROW EXECUTE FUNCTION photo.reject_content_update('tenant_id','id','kind','collection_id','source_revision','training_set_id','principal_type','principal_id','digest','item_count');

CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.snapshot_items FOR EACH ROW EXECUTE FUNCTION photo.reject_content_update('tenant_id','id','snapshot_id','ordinal','item_id','asset_version_id','artifact_id','sha256','payload');

CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.task_results FOR EACH ROW EXECUTE FUNCTION photo.reject_content_update('tenant_id','id','task_id','attempt','fencing_token','outputs','metrics','review_required','result_digest');

CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.artifacts FOR EACH ROW EXECUTE FUNCTION photo.reject_content_update('tenant_id','id','bucket','object_key','object_version','sha256','bytes','media_type','width','height','icc_sha256');

CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.overrides FOR EACH ROW EXECUTE FUNCTION photo.reject_content_update('tenant_id','id','asset_version_id','input_sha256','revision','config','config_hash','created_by');

CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.publications FOR EACH ROW EXECUTE FUNCTION photo.reject_content_update('tenant_id','id','item_id','asset_version_id');

-- +goose StatementBegin
CREATE FUNCTION photo.enforce_owner() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tid uuid; expected uuid; actual uuid; BEGIN IF TG_TABLE_NAME='tenants' THEN tid:=COALESCE(NEW.id,OLD.id); ELSE tid:=COALESCE(NEW.tenant_id,OLD.tenant_id); END IF; SELECT owner_user_id INTO expected FROM photo.tenants WHERE id=tid; IF NOT FOUND THEN RETURN NULL; END IF; SELECT user_id INTO actual FROM photo.memberships WHERE tenant_id=tid AND role='owner' AND status='active'; IF actual IS DISTINCT FROM expected THEN RAISE EXCEPTION 'tenant owner membership invariant' USING ERRCODE='23514'; END IF; RETURN NULL; END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.enforce_owner() OWNER TO photo_fn_owner;

CREATE CONSTRAINT TRIGGER owner_invariant AFTER INSERT OR UPDATE OR DELETE ON photo.tenants DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION photo.enforce_owner();

CREATE CONSTRAINT TRIGGER owner_invariant AFTER INSERT OR UPDATE OR DELETE ON photo.memberships DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION photo.enforce_owner();

REVOKE ALL ON ALL FUNCTIONS IN SCHEMA photo FROM PUBLIC;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
