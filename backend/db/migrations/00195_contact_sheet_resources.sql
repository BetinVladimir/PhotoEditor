-- Tenant-scoped immutable resources and contact-sheet interactive jobs.
-- +goose Up
CREATE TABLE photo.tenant_static_resources(
 tenant_id uuid NOT NULL,name text NOT NULL,artifact_id uuid NOT NULL,sha256 text NOT NULL,bytes bigint NOT NULL,bucket text NOT NULL,
 state text NOT NULL DEFAULT 'reserved',token uuid NOT NULL DEFAULT gen_random_uuid(),object_version text,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),completed_at timestamptz,
 PRIMARY KEY(tenant_id,name),UNIQUE(tenant_id,artifact_id),
 FOREIGN KEY(tenant_id) REFERENCES photo.tenants(id) DEFERRABLE INITIALLY DEFERRED,
 CHECK(name IN('contact_sheet_font_v1')),CHECK(sha256~'^[0-9a-f]{64}$'),CHECK(bytes BETWEEN 1 AND 10485760),CHECK(length(bucket) BETWEEN 3 AND 63),
 CHECK(state IN('reserved','ready')),CHECK((state='ready')=(object_version IS NOT NULL AND completed_at IS NOT NULL))
);
ALTER TABLE photo.tenant_static_resources ENABLE ROW LEVEL SECURITY;ALTER TABLE photo.tenant_static_resources FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.tenant_static_resources TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.tenant_static_resources TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_contact_sheet_font(p_tenant uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.tenant_static_resources%ROWTYPE;capacity numeric;used numeric;aid uuid:=gen_random_uuid();fence uuid:=gen_random_uuid();
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_sha!~'^[0-9a-f]{64}$' OR p_bytes NOT BETWEEN 1 AND 10485760 OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 IF NOT EXISTS(SELECT FROM photo.tenants t WHERE t.id=p_tenant AND t.status='active') THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.tenant_static_resources WHERE tenant_id=p_tenant AND name='contact_sheet_font_v1' FOR UPDATE;
 IF FOUND THEN
  IF r.sha256<>p_sha OR r.bytes<>p_bytes OR r.bucket<>p_bucket THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF r.state='ready' THEN RETURN QUERY SELECT 'replayed',r.artifact_id,r.token,true;RETURN;END IF;
  UPDATE photo.tenant_static_resources SET token=fence WHERE tenant_id=p_tenant AND name='contact_sheet_font_v1';RETURN QUERY SELECT 'resumed',r.artifact_id,fence,false;RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity THEN RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.tenant_static_resources(tenant_id,name,artifact_id,sha256,bytes,bucket,token) VALUES(p_tenant,'contact_sheet_font_v1',aid,p_sha,p_bytes,p_bucket,fence);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_contact_sheet_font(uuid,text,bigint,text) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.reserve_contact_sheet_font(uuid,text,bigint,text) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.reserve_contact_sheet_font(uuid,text,bigint,text) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_contact_sheet_font(p_tenant uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.tenant_static_resources%ROWTYPE;target text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_version IS NULL OR p_version IN('','null') THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.tenant_static_resources WHERE tenant_id=p_tenant AND name='contact_sheet_font_v1' FOR UPDATE;
 IF NOT FOUND OR r.artifact_id<>p_artifact OR r.token<>p_token OR r.sha256<>p_sha OR r.bytes<>p_bytes OR r.bucket<>p_bucket THEN RETURN false;END IF;
 IF r.state='ready' THEN RETURN r.object_version=p_version;END IF;
 target:='tenants/'||p_tenant||'/resources/'||p_artifact;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role) VALUES(p_artifact,p_tenant,p_bucket,target,p_version,p_sha,p_bytes,'font/ttf','ready',clock_timestamp(),'system_resource');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,storage_bytes=storage_bytes+p_bytes WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;IF NOT FOUND THEN RAISE EXCEPTION 'RESOURCE_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.tenant_static_resources SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE tenant_id=p_tenant AND name='contact_sheet_font_v1';RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_contact_sheet_font(uuid,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.complete_contact_sheet_font(uuid,uuid,uuid,text,bigint,text,text) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.complete_contact_sheet_font(uuid,uuid,uuid,text,bigint,text,text) TO photo_api;

ALTER TABLE photo.interactive_tasks DROP CONSTRAINT interactive_tasks_kind_check;
ALTER TABLE photo.interactive_tasks DROP CONSTRAINT interactive_tasks_operation_check;
ALTER TABLE photo.interactive_tasks ADD CONSTRAINT interactive_tasks_kind_check
 CHECK(kind IN('mask_manual','mask_prompt','generation','generation_preview','preview','contact_sheet'));
ALTER TABLE photo.interactive_tasks ADD CONSTRAINT interactive_tasks_operation_check
 CHECK(operation=CASE kind WHEN 'mask_manual' THEN 'mask.rasterize' WHEN 'mask_prompt' THEN 'mask.prompt' WHEN 'generation' THEN 'image.generate' WHEN 'contact_sheet' THEN 'report.contact_sheet' ELSE 'editor.preview' END);

-- The deletion function and DELETE privilege remain one closed allowlist.
GRANT DELETE ON photo.tenant_static_resources TO photo_fn_owner;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finish_tenant_deletion(p_job uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE;t text;
 tables_to_purge constant text[]:=ARRAY[
  'apply_v4_stages','apply_nodes','apply_builds','artifact_leases','asset_embeddings','asset_version_sources','bulk_item_results','collection_items',
  'dead_letters','defect_branches','derivatives','download_archive_plans','download_export_plans','download_intents','download_job_items','download_volumes','editor_drafts','export_batch_children','graph_expansions','ingest_catalog_items','ingest_decode_items','ingest_node_plans','ingest_originals','ingest_progress','ingest_rejections','interactive_tasks','media_retry_nodes','output_uploads','project_acl','publications','resource_reservations','resource_slots','result_cache','reviews','run_events','sections','share_sessions','snapshot_items','staging_cleanup','style_analysis_nodes','style_asset_bindings','style_candidates','style_import_builds','task_attempts','task_edges','task_results','training_pairs','upload_abort_cleanup','upload_parts','asset_versions','assets','backend_documents','bulk_operations','collections','download_build_state','download_jobs','export_batches','exports','ingest_upload_bindings','legacy_imports','media_retry_jobs','overrides','quota_reservations','share_links','snapshots','style_analysis_builds','style_versions','tasks','training_sets','upload_sessions','tenant_static_resources','artifacts','runs','styles','projects','export_presets','idempotency_records','inbox','invitations','outbox','scheduler_tenants','tenant_event_cursors','tenant_usage','worker_registrations'
 ];
BEGIN
 SELECT * INTO j FROM photo.deletion_jobs WHERE id=p_job AND resource_type='tenant' AND status='running' AND cleanup_token=p_token AND cleanup_lease_until>clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.tenants x WHERE x.id=j.tenant_id AND x.status='deleting') OR EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=j.tenant_id AND r.status IN('draft','queued','running','waiting_review','cancelling')) THEN RETURN false;END IF;
 SET CONSTRAINTS ALL DEFERRED;FOREACH t IN ARRAY tables_to_purge LOOP EXECUTE format('DELETE FROM photo.%I WHERE tenant_id=$1',t) USING j.tenant_id;END LOOP;
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
