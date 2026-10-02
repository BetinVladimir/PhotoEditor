-- Complete tenant deletion only after every explicitly enumerated tenant-owned
-- metadata table is empty. The tenant tombstone, suspended memberships,
-- lifecycle audit and the completed deletion job are retained.
-- +goose Up
-- photo_fn_owner owns only trusted SECURITY DEFINER functions. DELETE is
-- intentionally limited to the same closed allowlist used below; adding a new
-- tenant table requires an explicit migration and test update.
-- PostgreSQL also requires SELECT on columns referenced by DELETE predicates;
-- expose only tenant_id, not complete metadata rows.
GRANT SELECT (tenant_id), DELETE ON TABLE
 photo.apply_nodes,photo.apply_builds,photo.artifact_leases,photo.asset_embeddings,photo.asset_version_sources,photo.bulk_item_results,photo.collection_items,
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

-- A deleted tenant intentionally retains a suspended owner membership as an
-- audit tombstone. The ordinary active-owner invariant still applies to every
-- non-deleted tenant.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.enforce_owner() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tid uuid;expected uuid;actual uuid;tenant_status text;
BEGIN
 IF TG_TABLE_NAME='tenants' THEN tid:=COALESCE(NEW.id,OLD.id);ELSE tid:=COALESCE(NEW.tenant_id,OLD.tenant_id);END IF;
 SELECT owner_user_id,status INTO expected,tenant_status FROM photo.tenants WHERE id=tid;
 IF NOT FOUND OR tenant_status='deleted' THEN RETURN NULL;END IF;
 SELECT user_id INTO actual FROM photo.memberships WHERE tenant_id=tid AND role='owner' AND status='active';
 IF actual IS DISTINCT FROM expected THEN RAISE EXCEPTION 'tenant owner membership invariant' USING ERRCODE='23514';END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.enforce_owner() OWNER TO photo_fn_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finish_tenant_deletion(p_job uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE;t text;
 tables_to_purge constant text[]:=ARRAY[
  'apply_nodes','apply_builds','artifact_leases','asset_embeddings','asset_version_sources','bulk_item_results','collection_items',
  'dead_letters','defect_branches','derivatives','download_archive_plans','download_export_plans','download_intents','download_job_items',
  'download_volumes','editor_drafts','export_batch_children','graph_expansions','ingest_catalog_items','ingest_decode_items','ingest_node_plans',
  'ingest_originals','ingest_progress','ingest_rejections','interactive_tasks','media_retry_nodes','output_uploads','project_acl','publications',
  'resource_reservations','resource_slots','result_cache','reviews','run_events','sections','share_sessions','snapshot_items','staging_cleanup',
  'style_analysis_nodes','style_asset_bindings','style_candidates','style_import_builds','task_attempts','task_edges','task_results','training_pairs',
  'upload_abort_cleanup','upload_parts','asset_versions','assets','backend_documents','bulk_operations','collections','download_build_state',
  'download_jobs','export_batches','exports','ingest_upload_bindings','legacy_imports','media_retry_jobs','overrides','quota_reservations','share_links',
  'snapshots','style_analysis_builds','style_versions','tasks','training_sets','upload_sessions','artifacts','runs','styles','projects',
  'export_presets','idempotency_records','inbox','invitations','outbox','scheduler_tenants','tenant_event_cursors','tenant_usage','worker_registrations'
 ];
BEGIN
 SELECT * INTO j FROM photo.deletion_jobs WHERE id=p_job AND resource_type='tenant' AND status='running' AND cleanup_token=p_token AND cleanup_lease_until>clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.tenants x WHERE x.id=j.tenant_id AND x.status='deleting') OR EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=j.tenant_id AND r.status IN('draft','queued','running','waiting_review','cancelling')) THEN RETURN false;END IF;
 SET CONSTRAINTS ALL DEFERRED;
 FOREACH t IN ARRAY tables_to_purge LOOP EXECUTE format('DELETE FROM photo.%I WHERE tenant_id=$1',t) USING j.tenant_id;END LOOP;
 DELETE FROM photo.audit_log WHERE tenant_id=j.tenant_id AND action NOT LIKE 'tenant.%';
 UPDATE photo.memberships SET status='suspended',revision=revision+1 WHERE tenant_id=j.tenant_id AND status<>'suspended';
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
 VALUES(j.tenant_id,'system',NULL,'tenant.deleted','tenant',j.tenant_id,gen_random_uuid(),jsonb_build_object('job_id',j.id,'cleanup_passes',j.cleanup_passes+1));
 UPDATE photo.tenants SET status='deleted',revision=revision+1 WHERE id=j.tenant_id AND status='deleting';
 UPDATE photo.deletion_jobs SET status='completed',cleanup_token=NULL,cleanup_lease_until=NULL,cleanup_passes=cleanup_passes+1,cursor=NULL,error=NULL WHERE id=j.id;
 SET CONSTRAINTS ALL IMMEDIATE;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_tenant_deletion(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_tenant_deletion(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_tenant_deletion(uuid,uuid) TO photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
