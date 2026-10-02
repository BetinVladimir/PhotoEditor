-- Durable renderer-v4 plans. Existing builds remain graph v3; activation is a
-- separate code change after every v4 stage materializer is available.
-- +goose Up
ALTER TABLE photo.apply_builds ADD COLUMN graph_version int NOT NULL DEFAULT 3 CHECK(graph_version IN(3,4));

CREATE TABLE photo.apply_v4_stages(
 tenant_id uuid NOT NULL,
 run_id uuid NOT NULL,
 stage_index int NOT NULL CHECK(stage_index BETWEEN 0 AND 63),
 phase text NOT NULL CHECK(phase ~ '^[a-z][a-z0-9_]{0,63}$'),
 operation text,
 output_role text,
 cardinality text NOT NULL CHECK(cardinality IN('item','aggregate','review')),
 plan_hash text NOT NULL CHECK(plan_hash ~ '^[0-9a-f]{64}$'),
 PRIMARY KEY(tenant_id,run_id,stage_index),
 UNIQUE(tenant_id,run_id,phase),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.apply_builds(tenant_id,run_id) DEFERRABLE INITIALLY DEFERRED,
 CHECK((cardinality='review')=(operation IS NULL)),
 CHECK((operation IS NULL)=(output_role IS NULL))
);
ALTER TABLE photo.apply_v4_stages ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.apply_v4_stages FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.apply_v4_stages TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,DELETE ON photo.apply_v4_stages TO photo_fn_owner;

-- New tenant metadata is appended to the closed purge allowlist in the same
-- migration that creates it.
GRANT SELECT(tenant_id),DELETE ON photo.apply_v4_stages TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.register_apply_v4_plan(p_tenant uuid,p_run uuid,p_token uuid,p_hash text,p_plan jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;entry jsonb;i int:=0;old_hash text;
 allowed constant text[]:=ARRAY['mask.person','color.stats','style.resolve','image.denoise','pose.detect','geometry.body','mask.transform','image.color','series.measure','series.balance','image.local','face.detect','face.landmarks','face.parse','mask.face_skin','portrait.beauty','portrait.detail','defects.propose','defects.repair','background.compose','background.blur','image.inpaint','image.generate','composition.plan','geometry.crop','image.upscale','quality.check'];
BEGIN
 IF p_hash !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_plan)<>'array' OR jsonb_array_length(p_plan) NOT BETWEEN 4 AND 64 THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR b.graph_version<>4 OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() THEN RETURN false;END IF;
 SELECT min(plan_hash) INTO old_hash FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run;
 IF old_hash IS NOT NULL THEN RETURN old_hash=p_hash
  AND NOT EXISTS(SELECT FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND plan_hash<>p_hash)
  AND (SELECT count(*) FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run)=jsonb_array_length(p_plan)
  AND NOT EXISTS(SELECT FROM jsonb_array_elements(p_plan) WITH ORDINALITY x(value,ord)
   LEFT JOIN photo.apply_v4_stages s ON s.tenant_id=p_tenant AND s.run_id=p_run AND s.stage_index=x.ord-1
   WHERE s.stage_index IS NULL OR s.phase<>x.value->>'phase' OR coalesce(s.operation,'')<>coalesce(x.value->>'operation','')
    OR coalesce(s.output_role,'')<>coalesce(x.value->>'output_role','') OR s.cardinality<>x.value->>'cardinality');END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_plan) LOOP
  IF (entry-'phase'-'operation'-'output_role'-'cardinality')<>'{}' OR entry->>'phase' !~ '^[a-z][a-z0-9_]{0,63}$' OR entry->>'cardinality' NOT IN('item','aggregate','review') THEN RETURN false;END IF;
  IF entry->>'cardinality'='review' THEN IF coalesce(entry->>'operation','')<>'' OR coalesce(entry->>'output_role','')<>'' THEN RETURN false;END IF;
  ELSIF NOT ((entry->>'operation')=ANY(allowed)) OR NOT EXISTS(SELECT FROM photo.operation_registry o WHERE o.operation=entry->>'operation' AND o.version='1') OR coalesce(entry->>'output_role','')='' THEN RETURN false;END IF;
  INSERT INTO photo.apply_v4_stages(tenant_id,run_id,stage_index,phase,operation,output_role,cardinality,plan_hash)
  VALUES(p_tenant,p_run,i,entry->>'phase',NULLIF(entry->>'operation',''),NULLIF(entry->>'output_role',''),entry->>'cardinality',p_hash);i:=i+1;
 END LOOP;
 IF NOT EXISTS(SELECT FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=0 AND phase IN('mask','resolve'))
 OR NOT EXISTS(SELECT FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=i-1 AND phase='quality') THEN RAISE EXCEPTION 'INVALID_APPLY_V4_BOUNDARIES' USING ERRCODE='23514';END IF;
 UPDATE photo.apply_builds SET stage_index=0,phase=(p_plan->0->>'phase'),updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run AND graph_version=4;
 RETURN true;
EXCEPTION WHEN unique_violation OR check_violation THEN RETURN false;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) TO photo_scheduler;

-- Extend the purge function's allowlist without widening any other principal.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finish_tenant_deletion(p_job uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE;t text;
 tables_to_purge constant text[]:=ARRAY[
  'apply_v4_stages','apply_nodes','apply_builds','artifact_leases','asset_embeddings','asset_version_sources','bulk_item_results','collection_items',
  'dead_letters','defect_branches','derivatives','download_archive_plans','download_export_plans','download_intents','download_job_items','download_volumes','editor_drafts','export_batch_children','graph_expansions','ingest_catalog_items','ingest_decode_items','ingest_node_plans','ingest_originals','ingest_progress','ingest_rejections','interactive_tasks','media_retry_nodes','output_uploads','project_acl','publications','resource_reservations','resource_slots','result_cache','reviews','run_events','sections','share_sessions','snapshot_items','staging_cleanup','style_analysis_nodes','style_asset_bindings','style_candidates','style_import_builds','task_attempts','task_edges','task_results','training_pairs','upload_abort_cleanup','upload_parts','asset_versions','assets','backend_documents','bulk_operations','collections','download_build_state','download_jobs','export_batches','exports','ingest_upload_bindings','legacy_imports','media_retry_jobs','overrides','quota_reservations','share_links','snapshots','style_analysis_builds','style_versions','tasks','training_sets','upload_sessions','artifacts','runs','styles','projects','export_presets','idempotency_records','inbox','invitations','outbox','scheduler_tenants','tenant_event_cursors','tenant_usage','worker_registrations'
 ];
BEGIN
 SELECT * INTO j FROM photo.deletion_jobs WHERE id=p_job AND resource_type='tenant' AND status='running' AND cleanup_token=p_token AND cleanup_lease_until>clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.tenants x WHERE x.id=j.tenant_id AND x.status='deleting') OR EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=j.tenant_id AND r.status IN('draft','queued','running','waiting_review','cancelling')) THEN RETURN false;END IF;
 SET CONSTRAINTS ALL DEFERRED;
 FOREACH t IN ARRAY tables_to_purge LOOP EXECUTE format('DELETE FROM photo.%I WHERE tenant_id=$1',t) USING j.tenant_id;END LOOP;
 DELETE FROM photo.audit_log WHERE tenant_id=j.tenant_id AND action NOT LIKE 'tenant.%';
 UPDATE photo.memberships SET status='suspended',revision=revision+1 WHERE tenant_id=j.tenant_id AND status<>'suspended';
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES(j.tenant_id,'system',NULL,'tenant.deleted','tenant',j.tenant_id,gen_random_uuid(),jsonb_build_object('job_id',j.id,'cleanup_passes',j.cleanup_passes+1));
 UPDATE photo.tenants SET status='deleted',revision=revision+1 WHERE id=j.tenant_id AND status='deleting';
 UPDATE photo.deletion_jobs SET status='completed',cleanup_token=NULL,cleanup_lease_until=NULL,cleanup_passes=cleanup_passes+1,cursor=NULL,error=NULL WHERE id=j.id;
 SET CONSTRAINTS ALL IMMEDIATE;RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_tenant_deletion(uuid,uuid) OWNER TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
