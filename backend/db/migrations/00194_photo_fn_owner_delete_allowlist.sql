-- Correct the legacy broad DELETE grant from 00010. photo_fn_owner may delete
-- tenant metadata only from this closed allowlist, through trusted functions.
-- Adding a purgeable tenant table requires a forward migration and a privilege
-- regression-test update.
-- +goose Up
REVOKE DELETE ON ALL TABLES IN SCHEMA photo FROM photo_fn_owner;

GRANT DELETE ON TABLE
 photo.audit_log,photo.apply_v4_stages,photo.apply_nodes,photo.apply_builds,photo.artifact_leases,photo.asset_embeddings,photo.asset_version_sources,photo.bulk_item_results,photo.collection_items,
 photo.dead_letters,photo.defect_branches,photo.derivatives,photo.download_archive_plans,photo.download_export_plans,photo.download_intents,photo.download_job_items,
 photo.download_volumes,photo.editor_drafts,photo.export_batch_children,photo.graph_expansions,photo.ingest_catalog_items,photo.ingest_decode_items,photo.ingest_node_plans,
 photo.ingest_originals,photo.ingest_progress,photo.ingest_rejections,photo.interactive_tasks,photo.media_retry_nodes,photo.output_uploads,photo.project_acl,photo.publications,
 photo.resource_reservations,photo.resource_slots,photo.result_cache,photo.reviews,photo.run_events,photo.sections,photo.share_sessions,photo.snapshot_items,photo.staging_cleanup,
 photo.style_analysis_nodes,photo.style_asset_bindings,photo.style_candidates,photo.style_import_builds,photo.task_attempts,photo.task_edges,photo.task_results,photo.training_pairs,
 photo.upload_abort_cleanup,photo.upload_parts,photo.asset_versions,photo.assets,photo.backend_documents,photo.bulk_operations,photo.collections,photo.download_build_state,
 photo.download_jobs,photo.export_batches,photo.exports,photo.ingest_upload_bindings,photo.legacy_imports,photo.media_retry_jobs,photo.overrides,photo.quota_reservations,photo.share_links,
 photo.snapshots,photo.style_analysis_builds,photo.style_versions,photo.tasks,photo.training_sets,photo.upload_sessions,photo.artifacts,photo.runs,photo.styles,photo.projects,
 photo.export_presets,photo.idempotency_records,photo.inbox,photo.invitations,photo.outbox,photo.scheduler_tenants,photo.tenant_event_cursors,photo.tenant_usage,photo.worker_registrations
TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
