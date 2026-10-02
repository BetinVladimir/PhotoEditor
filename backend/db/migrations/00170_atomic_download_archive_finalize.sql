-- +goose Up
-- Validate every canonical archive result before publishing any volume. The
-- previous single-pass loop could return false after earlier volume UPDATEs.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finalize_download_archives(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;j photo.download_jobs%ROWTYPE;b photo.download_build_state%ROWTYPE;until_time timestamptz;v record;archive photo.artifacts%ROWTYPE;manifest photo.artifacts%ROWTYPE;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO j FROM photo.download_jobs WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO b FROM photo.download_build_state WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF r.id IS NULL OR j.id IS NULL OR b.run_id IS NULL OR b.phase<>'archiving' OR NOT r.graph_sealed OR r.cancel_requested OR j.cancel_requested OR r.status<>'running' OR j.status<>'building' THEN RETURN false;END IF;
 IF EXISTS(SELECT FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='archive.build' AND state NOT IN ('succeeded','failed','cancelled','skipped')) THEN RETURN false;END IF;
 IF EXISTS(SELECT FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='archive.build' AND state<>'succeeded') THEN
  UPDATE photo.runs SET status='failed',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
  UPDATE photo.download_jobs SET status='failed' WHERE tenant_id=p_tenant AND id=j.id;RETURN true;
 END IF;

 -- Phase one is read-only apart from row locks. A missing or malformed result
 -- leaves every volume queued and the parent job non-ready.
 FOR v IN SELECT volume.id,volume.volume_no,p.id task_id FROM photo.download_volumes volume
  JOIN photo.download_archive_plans p ON p.tenant_id=volume.tenant_id AND p.run_id=p_run AND p.volume_no=volume.volume_no
  WHERE volume.tenant_id=p_tenant AND volume.download_job_id=j.id AND volume.status='queued' ORDER BY volume.volume_no FOR UPDATE OF volume LOOP
  archive:=NULL;manifest:=NULL;
  SELECT a.* INTO archive FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.output_role='archive' AND a.state='ready'
   WHERE t.tenant_id=p_tenant AND t.id=v.task_id AND t.state='succeeded' AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256));
  SELECT a.* INTO manifest FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.output_role='archive_manifest' AND a.state='ready'
   WHERE t.tenant_id=p_tenant AND t.id=v.task_id AND t.state='succeeded' AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256));
  IF archive.id IS NULL OR manifest.id IS NULL OR archive.media_type<>'application/zip' OR archive.bytes<1 OR archive.object_version IS NULL OR archive.object_version IN ('','null')
   OR manifest.bytes<1 OR manifest.object_version IS NULL OR manifest.object_version IN ('','null') THEN RETURN false;END IF;
 END LOOP;

 -- Phase two publishes only after the complete result set passed validation.
 FOR v IN SELECT volume.id,volume.volume_no,p.id task_id FROM photo.download_volumes volume
  JOIN photo.download_archive_plans p ON p.tenant_id=volume.tenant_id AND p.run_id=p_run AND p.volume_no=volume.volume_no
  WHERE volume.tenant_id=p_tenant AND volume.download_job_id=j.id AND volume.status='queued' ORDER BY volume.volume_no FOR UPDATE OF volume LOOP
  SELECT a.* INTO archive FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.output_role='archive' AND a.state='ready'
   WHERE t.tenant_id=p_tenant AND t.id=v.task_id AND t.state='succeeded' AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256));
  SELECT a.* INTO manifest FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.output_role='archive_manifest' AND a.state='ready'
   WHERE t.tenant_id=p_tenant AND t.id=v.task_id AND t.state='succeeded' AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256));
  UPDATE photo.download_volumes SET artifact_id=archive.id,manifest_artifact_id=manifest.id,status='ready',
   files_done=(SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=p_tenant AND i.download_job_id=j.id AND i.volume_no=v.volume_no),bytes_written=archive.bytes
  WHERE tenant_id=p_tenant AND id=v.id;
 END LOOP;
 IF EXISTS(SELECT FROM photo.download_volumes WHERE tenant_id=p_tenant AND download_job_id=j.id AND status<>'ready') THEN RETURN false;END IF;
 until_time:=clock_timestamp()+interval '24 hours';
 UPDATE photo.artifacts SET expires_at=until_time WHERE tenant_id=p_tenant AND id IN (
  SELECT artifact_id FROM photo.download_volumes WHERE tenant_id=p_tenant AND download_job_id=j.id
  UNION SELECT manifest_artifact_id FROM photo.download_volumes WHERE tenant_id=p_tenant AND download_job_id=j.id AND manifest_artifact_id IS NOT NULL);
 UPDATE photo.download_jobs SET status='ready',expires_at=until_time WHERE tenant_id=p_tenant AND id=j.id;
 UPDATE photo.runs SET status='completed',revision=revision+1,download_build_token=NULL,download_build_until=NULL WHERE tenant_id=p_tenant AND id=p_run;RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_download_archives(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_download_archives(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_download_archives(uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
