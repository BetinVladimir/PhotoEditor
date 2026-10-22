-- +goose Up
-- continue_items completes individual encodes; the download itself is all-or-error.
ALTER TABLE photo.download_jobs ADD COLUMN failure jsonb;
ALTER TABLE photo.download_jobs ADD CONSTRAINT download_failure_object CHECK(failure IS NULL OR jsonb_typeof(failure)='object');
-- Keep every planned outcome visible: invalid encoded metadata is an unavailable
-- output, not a disappearing inventory member.
-- +goose StatementBegin
DO $$ DECLARE source text;anchor text;replacement text; BEGIN
 source:=pg_get_functiondef('photo.download_encode_outcomes(uuid,uuid)'::regprocedure);
 anchor:=$old$AND j.status='building' AND NOT j.cancel_requested AND (a.id IS NULL OR (a.bytes>0 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')))$old$;
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'download outcome availability source drift';END IF;
 source:=replace(source,anchor,$new$AND j.status='building' AND NOT j.cancel_requested$new$);
 anchor:=$old$a.output_role='encoded_image' AND a.state='ready'$old$;
 replacement:=anchor||$new$ AND a.bytes>0 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')$new$;
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'download encoded metadata source drift';END IF;
 EXECUTE replace(source,anchor,replacement);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.fail_download_encoding(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;j photo.download_jobs%ROWTYPE;b photo.download_build_state%ROWTYPE;report jsonb;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO j FROM photo.download_jobs WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO b FROM photo.download_build_state WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF r.id IS NULL OR j.id IS NULL OR b.run_id IS NULL OR r.kind<>'download' OR b.phase<>'encoding'
 OR r.cancel_requested OR j.cancel_requested OR NOT r.graph_sealed THEN RETURN false;END IF;
 IF r.status='failed' AND j.status='failed' AND j.failure->>'code'='DOWNLOAD_EXPORT_FAILED' THEN RETURN true;END IF;
 IF r.status<>'running' OR j.status<>'building' THEN RETURN false;END IF;
 PERFORM t.id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.operation='export.encode' ORDER BY t.id FOR SHARE;
 IF (SELECT count(*) FROM photo.download_export_plans WHERE tenant_id=p_tenant AND run_id=p_run)<>b.item_count
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='export.encode')<>b.item_count
 OR (SELECT count(*) FROM photo.snapshot_items WHERE tenant_id=p_tenant AND snapshot_id=r.input_snapshot_id)<>b.item_count
 OR EXISTS(SELECT FROM photo.download_export_plans p LEFT JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.id AND t.run_id=p.run_id AND t.operation='export.encode'
  WHERE p.tenant_id=p_tenant AND p.run_id=p_run AND (t.id IS NULL OR t.state NOT IN ('succeeded','failed','cancelled','skipped')))
 OR EXISTS(SELECT FROM photo.download_export_plans p LEFT JOIN photo.snapshot_items si ON si.tenant_id=p.tenant_id AND si.snapshot_id=r.input_snapshot_id AND si.ordinal=p.ordinal
  WHERE p.tenant_id=p_tenant AND p.run_id=p_run AND (si.id IS NULL OR si.asset_version_id IS NULL)) THEN RETURN false;END IF;
 SELECT jsonb_build_object('code','DOWNLOAD_EXPORT_FAILED','items',jsonb_agg(jsonb_build_object(
  'ordinal',p.ordinal,'asset_version_id',si.asset_version_id,'code',CASE t.state WHEN 'failed' THEN 'EXPORT_FAILED' WHEN 'cancelled' THEN 'EXPORT_CANCELLED' WHEN 'succeeded' THEN 'EXPORT_OUTPUT_UNAVAILABLE' ELSE 'EXPORT_SKIPPED' END) ORDER BY p.ordinal))
 INTO report FROM photo.download_export_plans p JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.id AND t.run_id=p.run_id
 JOIN photo.snapshot_items si ON si.tenant_id=p.tenant_id AND si.snapshot_id=r.input_snapshot_id AND si.ordinal=p.ordinal
 LEFT JOIN photo.download_encode_outcomes(p_tenant,p_run) outcome ON outcome.ordinal=p.ordinal
 WHERE p.tenant_id=p_tenant AND p.run_id=p_run AND (t.state IN ('failed','cancelled','skipped') OR (t.state='succeeded' AND outcome.artifact_id IS NULL));
 IF report->'items' IS NULL OR report->'items'='null'::jsonb THEN RETURN false;END IF;
 UPDATE photo.download_jobs SET status='failed',failure=report WHERE tenant_id=p_tenant AND id=j.id;
 UPDATE photo.runs SET status='failed',revision=revision+1,download_build_token=NULL,download_build_until=NULL WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.fail_download_encoding(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_download_encoding(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.fail_download_encoding(uuid,uuid) TO photo_scheduler;
-- Preserve the existing guest authorization and exact principal/policy scope.
-- +goose StatementBegin
DO $$ DECLARE source text;anchor text:='j.expires_at,NULL::jsonb;'; BEGIN
 source:=pg_get_functiondef('photo.get_portal_download(bytea,uuid)'::regprocedure);
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'download status source drift';END IF;
 source:=replace(source,anchor,'j.expires_at,j.failure;');
 IF (length(source)-length(replace(source,$old$(SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id AND i.artifact_id IS NOT NULL)$old$,'')))/length($old$(SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id AND i.artifact_id IS NOT NULL)$old$)<>1 THEN RAISE EXCEPTION 'download progress source drift';END IF;
 source:=replace(source,$old$(SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id AND i.artifact_id IS NOT NULL)$old$,$new$greatest((SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id AND i.artifact_id IS NOT NULL),(SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=j.tenant_id AND t.run_id=j.run_id AND t.operation='export.encode' AND t.state IN ('succeeded','failed','cancelled','skipped')))$new$);
 IF (length(source)-length(replace(source,$old$(SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id)$old$,'')))/length($old$(SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id)$old$)<>1 THEN RAISE EXCEPTION 'download progress source drift';END IF;
 source:=replace(source,$old$(SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id)$old$,$new$coalesce((SELECT s.item_count FROM photo.snapshots s WHERE s.tenant_id=j.tenant_id AND s.id=j.snapshot_id),(SELECT count(*) FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id))$new$);
 EXECUTE source;
END $$;
-- +goose StatementEnd
-- Archive failures carry the same safe per-item report; no partial publication.
-- +goose StatementBegin
DO $$ DECLARE source text;anchor text;replacement text; BEGIN
 source:=pg_get_functiondef('photo.finalize_download_archives(uuid,uuid)'::regprocedure);
 anchor:=$old$  UPDATE photo.runs SET status='failed',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
  UPDATE photo.download_jobs SET status='failed' WHERE tenant_id=p_tenant AND id=j.id;RETURN true;$old$;
 replacement:=$new$  UPDATE photo.download_jobs SET status='failed',failure=jsonb_build_object('code','DOWNLOAD_ARCHIVE_FAILED','items',(
   SELECT jsonb_agg(jsonb_build_object('ordinal',i.ordinal,'asset_version_id',i.asset_version_id,'code',
    CASE WHEN t.state='failed' AND attempt.error->'args'->>'artifact_id'=i.artifact_id::text AND attempt.error->'args'->>'sha256'=i.sha256
     AND attempt.error->>'code' IN ('ARTIFACT_MISSING','ARTIFACT_HASH_MISMATCH') THEN attempt.error->>'code'
    ELSE CASE t.state WHEN 'failed' THEN 'ARCHIVE_FAILED' WHEN 'cancelled' THEN 'ARCHIVE_CANCELLED' ELSE 'ARCHIVE_SKIPPED' END END) ORDER BY i.ordinal)
   FROM photo.download_job_items i JOIN photo.download_archive_plans p ON p.tenant_id=i.tenant_id AND p.run_id=p_run AND p.volume_no=i.volume_no
   JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.id AND t.run_id=p.run_id AND t.operation='archive.build'
   LEFT JOIN photo.task_attempts attempt ON attempt.tenant_id=t.tenant_id AND attempt.task_id=t.id AND attempt.attempt=t.attempt AND attempt.fencing_token=t.fencing_token AND attempt.finished_at IS NOT NULL
   WHERE i.tenant_id=p_tenant AND i.download_job_id=j.id AND t.state IN ('failed','cancelled','skipped')))
  WHERE tenant_id=p_tenant AND id=j.id;
  UPDATE photo.runs SET status='failed',revision=revision+1,download_build_token=NULL,download_build_until=NULL WHERE tenant_id=p_tenant AND id=p_run;RETURN true;$new$;
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'download archive failure source drift';END IF;
 source:=replace(source,anchor,replacement);
 anchor:=$old$ IF EXISTS(SELECT FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='archive.build' AND state NOT IN ('succeeded','failed','cancelled','skipped')) THEN RETURN false;END IF;$old$;
 replacement:=$new$ PERFORM t.id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.operation='archive.build' ORDER BY t.id FOR SHARE;
 IF EXISTS(SELECT FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='archive.build' AND state NOT IN ('succeeded','failed','cancelled','skipped')) THEN RETURN false;END IF;$new$;
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'download archive task lock source drift';END IF;
 EXECUTE replace(source,anchor,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
