-- +goose Up
-- Keep atomic publication while fencing each derivative to its declared variant.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finalize_media_retry_job() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;j photo.media_retry_jobs%ROWTYPE;n photo.media_retry_nodes%ROWTYPE;
 t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;a photo.artifacts%ROWTYPE;m photo.artifacts%ROWTYPE;failed boolean;expected integer;
BEGIN
 SELECT job.tenant_id,job.event_id,v.asset_id,asset.project_id INTO candidate FROM photo.media_retry_jobs job
 JOIN photo.asset_versions v ON v.tenant_id=job.tenant_id AND v.id=job.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.runs source ON source.tenant_id=v.tenant_id AND source.id=v.created_by_run_id
 WHERE job.status='running' AND source.config_snapshot ? 'shown_semantic'
 AND NOT EXISTS(SELECT 1 FROM photo.tasks x WHERE x.tenant_id=job.tenant_id AND x.run_id=job.run_id
  AND x.state NOT IN('succeeded','failed','cancelled','skipped'))
 ORDER BY job.created_at,job.event_id LIMIT 1;
 IF NOT FOUND THEN RETURN photo.finalize_media_retry_job_pre_shown_source();END IF;
 PERFORM id FROM photo.tenants WHERE id=candidate.tenant_id FOR UPDATE;
 PERFORM id FROM photo.projects WHERE tenant_id=candidate.tenant_id AND id=candidate.project_id FOR UPDATE;
 PERFORM id FROM photo.assets WHERE tenant_id=candidate.tenant_id AND id=candidate.asset_id FOR UPDATE;
 SELECT * INTO j FROM photo.media_retry_jobs WHERE event_id=candidate.event_id AND status='running' FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=j.tenant_id AND id=j.run_id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=j.tenant_id AND run_id=j.run_id ORDER BY id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=j.tenant_id AND run_id=j.run_id AND state NOT IN('succeeded','failed','cancelled','skipped')) THEN RETURN false;END IF;
 expected:=jsonb_array_length(j.variants)*2;
 failed:=NOT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=j.tenant_id AND id=j.run_id AND status='running' AND graph_sealed AND NOT cancel_requested)
 OR expected NOT BETWEEN 2 AND 4
 OR (SELECT count(*) FROM photo.media_retry_nodes WHERE tenant_id=j.tenant_id AND run_id=j.run_id)<>expected
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=j.tenant_id AND run_id=j.run_id)<>expected
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=j.tenant_id AND run_id=j.run_id AND state<>'succeeded');
 IF NOT failed THEN
  BEGIN
   FOR n IN SELECT * FROM photo.media_retry_nodes WHERE tenant_id=j.tenant_id AND run_id=j.run_id ORDER BY variant LOOP
    IF n.asset_version_id IS DISTINCT FROM j.asset_version_id OR n.variant NOT IN('thumb320','thumb640','preview1600','preview2560') THEN RAISE EXCEPTION 'NODE_SCOPE_INVALID' USING ERRCODE='P2758';END IF;
    IF photo.semantic_shown_media_task_source_live(n.id) IS DISTINCT FROM true THEN RAISE EXCEPTION 'SOURCE_STALE' USING ERRCODE='P2758';END IF;
    SELECT * INTO t FROM photo.tasks WHERE tenant_id=j.tenant_id AND id=n.id AND state='succeeded' AND canonical_result_id IS NOT NULL;
    SELECT * INTO z FROM photo.task_results WHERE tenant_id=j.tenant_id AND id=t.canonical_result_id AND task_id=t.id AND attempt=t.attempt AND fencing_token=t.fencing_token;
    SELECT x.* INTO a FROM photo.artifacts x WHERE x.tenant_id=j.tenant_id AND x.created_by_task_id=t.id
     AND x.produced_attempt=t.attempt AND x.produced_fence=t.fencing_token AND x.output_role='derivative'
     AND x.state='ready' AND x.verified_at IS NOT NULL AND x.object_version IS NOT NULL AND x.object_version NOT IN('','null')
     AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) AND x.bytes>0 AND x.width>0 AND x.height>0
     AND greatest(x.width,x.height)<=CASE n.variant WHEN 'thumb320' THEN 320 WHEN 'thumb640' THEN 640 WHEN 'preview1600' THEN 1600 WHEN 'preview2560' THEN 2560 ELSE 0 END
     AND x.media_type IN('image/jpeg','image/webp') AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',x.id,'sha256',x.sha256));
    SELECT x.* INTO m FROM photo.artifacts x WHERE x.tenant_id=j.tenant_id AND x.created_by_task_id=t.id
     AND x.produced_attempt=t.attempt AND x.produced_fence=t.fencing_token AND x.output_role='derivative_manifest'
     AND x.state='ready' AND x.verified_at IS NOT NULL AND x.object_version IS NOT NULL AND x.object_version NOT IN('','null')
     AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) AND x.bytes BETWEEN 2 AND 1048576
     AND x.media_type='application/json' AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',x.id,'sha256',x.sha256));
    IF a.id IS NULL OR m.id IS NULL OR jsonb_array_length(z.outputs) IS DISTINCT FROM 2 THEN RAISE EXCEPTION 'OUTPUT_INVALID' USING ERRCODE='P2758';END IF;
    INSERT INTO photo.derivatives(tenant_id,asset_version_id,variant,profile_hash,artifact_id,status,task_id)
     VALUES(j.tenant_id,j.asset_version_id,n.variant,n.profile_hash,a.id,'ready',t.id)
     ON CONFLICT(tenant_id,asset_version_id,variant,profile_hash) DO UPDATE SET artifact_id=EXCLUDED.artifact_id,status='ready',task_id=EXCLUDED.task_id;
   END LOOP;
  EXCEPTION WHEN SQLSTATE 'P2758' THEN failed:=true; -- rolls back every family insert
  END;
 END IF;
 UPDATE photo.media_retry_jobs SET status=CASE WHEN failed THEN 'failed' ELSE 'completed' END,
  error_code=CASE WHEN failed THEN 'DERIVATIVE_FAILED' ELSE NULL END WHERE event_id=j.event_id;
 UPDATE photo.runs SET status=CASE WHEN failed THEN 'failed' ELSE 'completed' END,revision=revision+1
  WHERE tenant_id=j.tenant_id AND id=j.run_id;
 RETURN true;
END $$;
ALTER FUNCTION photo.finalize_media_retry_job() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_media_retry_job() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_media_retry_job() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
