-- +goose Up
-- Adopt only artifacts from each task's canonical fenced attempt. A retry job
-- becomes terminal only after every variant is terminal, so the derivative
-- cache never points at a partially generated family.
-- +goose StatementBegin
CREATE FUNCTION photo.finalize_media_retry_job() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.media_retry_jobs%ROWTYPE;n photo.media_retry_nodes%ROWTYPE;task_row photo.tasks%ROWTYPE;result_row photo.task_results%ROWTYPE;a photo.artifacts%ROWTYPE;m photo.artifacts%ROWTYPE;failed boolean;ordinal_ref bigint;summary jsonb:='[]'::jsonb;
BEGIN
 SELECT * INTO j FROM photo.media_retry_jobs x WHERE x.status='running'
  AND NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=x.tenant_id AND t.run_id=x.run_id AND t.state NOT IN('succeeded','failed','cancelled'))
  ORDER BY x.created_at,x.event_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN false;END IF;
 failed:=EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=j.tenant_id AND t.run_id=j.run_id AND t.state<>'succeeded');
 IF NOT failed THEN
  FOR n IN SELECT * FROM photo.media_retry_nodes WHERE tenant_id=j.tenant_id AND run_id=j.run_id ORDER BY variant LOOP
   SELECT * INTO task_row FROM photo.tasks WHERE tenant_id=j.tenant_id AND id=n.id AND state='succeeded' AND canonical_result_id IS NOT NULL;
   SELECT * INTO result_row FROM photo.task_results WHERE tenant_id=j.tenant_id AND id=task_row.canonical_result_id AND task_id=task_row.id;
   SELECT x.* INTO a FROM photo.artifacts x WHERE x.tenant_id=j.tenant_id AND x.created_by_task_id=task_row.id AND x.produced_attempt=result_row.attempt AND x.produced_fence=result_row.fencing_token AND x.output_role='derivative' AND x.state='ready' AND x.object_version IS NOT NULL AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) AND result_row.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',x.id,'sha256',x.sha256));
   SELECT x.* INTO m FROM photo.artifacts x WHERE x.tenant_id=j.tenant_id AND x.created_by_task_id=task_row.id AND x.produced_attempt=result_row.attempt AND x.produced_fence=result_row.fencing_token AND x.output_role='derivative_manifest' AND x.state='ready' AND x.object_version IS NOT NULL AND result_row.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',x.id,'sha256',x.sha256));
   IF a.id IS NULL OR m.id IS NULL OR jsonb_array_length(result_row.outputs)<>2 THEN failed:=true;EXIT;END IF;
   INSERT INTO photo.derivatives(tenant_id,asset_version_id,variant,profile_hash,artifact_id,status,task_id) VALUES(j.tenant_id,j.asset_version_id,n.variant,n.profile_hash,a.id,'ready',task_row.id)
    ON CONFLICT(tenant_id,asset_version_id,variant,profile_hash) DO UPDATE SET artifact_id=EXCLUDED.artifact_id,status='ready',task_id=EXCLUDED.task_id;
   summary:=summary||jsonb_build_array(jsonb_build_object('variant',n.variant,'artifact_id',a.id,'manifest_artifact_id',m.id));
  END LOOP;
 END IF;
 SELECT si.ordinal INTO ordinal_ref FROM photo.bulk_operations b JOIN photo.snapshot_items si ON si.tenant_id=b.tenant_id AND si.snapshot_id=b.snapshot_id AND si.item_id=j.item_id AND si.asset_version_id=j.asset_version_id WHERE b.tenant_id=j.tenant_id AND b.id=j.bulk_operation_id;
 IF failed THEN
  UPDATE photo.media_retry_jobs SET status='failed',error_code='DERIVATIVE_FAILED' WHERE event_id=j.event_id;
  UPDATE photo.runs SET status='failed',revision=revision+1 WHERE tenant_id=j.tenant_id AND id=j.run_id;
  UPDATE photo.bulk_item_results SET status='failed',error_code='DERIVATIVE_FAILED',result=jsonb_build_object('media_retry_event_id',j.event_id) WHERE tenant_id=j.tenant_id AND bulk_operation_id=j.bulk_operation_id AND snapshot_ordinal=ordinal_ref;
  UPDATE photo.bulk_operations SET status='completed_with_errors' WHERE tenant_id=j.tenant_id AND id=j.bulk_operation_id;
  UPDATE photo.runs SET status='completed_with_errors',revision=revision+1 WHERE tenant_id=j.tenant_id AND id=(SELECT run_id FROM photo.bulk_operations WHERE tenant_id=j.tenant_id AND id=j.bulk_operation_id) AND status='completed';
 ELSE
  UPDATE photo.media_retry_jobs SET status='completed',error_code=NULL WHERE event_id=j.event_id;
  UPDATE photo.runs SET status='completed',revision=revision+1 WHERE tenant_id=j.tenant_id AND id=j.run_id;
  UPDATE photo.bulk_item_results SET status='succeeded',error_code=NULL,result=jsonb_build_object('media_retry_event_id',j.event_id,'derivatives',summary) WHERE tenant_id=j.tenant_id AND bulk_operation_id=j.bulk_operation_id AND snapshot_ordinal=ordinal_ref;
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_media_retry_job() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_media_retry_job() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_media_retry_job() TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
