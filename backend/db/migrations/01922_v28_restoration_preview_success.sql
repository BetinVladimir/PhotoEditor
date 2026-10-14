-- +goose Up
-- Recovery source for the gap between fourth receipt and run completion.
-- +goose StatementBegin
CREATE FUNCTION photo.pending_v28_restoration_preview_completion(p_tenant uuid,p_after uuid)
RETURNS TABLE(run_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.id FROM photo.runs r
 WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  AND r.tenant_id=p_tenant AND r.v28_restoration_preview_task_id IS NOT NULL
  AND r.kind='preview' AND r.status='running' AND r.graph_sealed
  AND NOT r.cancel_requested AND (p_after IS NULL OR r.id>p_after)
  AND (SELECT count(*) FROM photo.backend_documents d
   WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.purpose='task_params'
    AND d.state='ready' AND d.v28_restoration_preview_params IS NOT NULL
    AND d.v28_restoration_preview_derivative_receipt IS NOT NULL)=4
 ORDER BY r.id LIMIT 32;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.pending_v28_restoration_preview_completion(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pending_v28_restoration_preview_completion(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.pending_v28_restoration_preview_completion(uuid,uuid) TO photo_scheduler;

-- Every immutable physical receipt is revalidated under the source/run/task
-- locks before the only transition that makes the preview publicly visible.
-- +goose StatementBegin
CREATE FUNCTION photo.finalize_successful_v28_restoration_preview(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;s record;d photo.backend_documents%ROWTYPE;
 side text;variant text;receipt jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.v28_restoration_preview_task_id IS NULL OR r.kind<>'preview'
  OR r.status<>'running' OR NOT r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.v28_restoration_preview_source(p_tenant,r.v28_restoration_preview_task_id);
 IF NOT FOUND OR r.config_snapshot->>'source_review_id' IS DISTINCT FROM s.review_id::text
  OR r.config_snapshot->>'source_result_id' IS DISTINCT FROM s.result_id::text
  OR r.config_snapshot->>'source_attempt' IS DISTINCT FROM s.attempt::text
  OR r.config_snapshot->>'source_fence' IS DISTINCT FROM s.fence::text
  OR r.config_snapshot->'root' IS DISTINCT FROM jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha)
  OR r.config_snapshot->'before' IS DISTINCT FROM jsonb_build_object('artifact_id',s.source_id,'sha256',s.source_sha)
  OR r.config_snapshot->'after' IS DISTINCT FROM jsonb_build_object('artifact_id',s.candidate_id,'sha256',s.candidate_sha)
 THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=s.project_id FOR SHARE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=s.run_id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND
  (id=r.v28_restoration_preview_task_id OR run_id=p_run) ORDER BY id FOR UPDATE;
 PERFORM id FROM photo.reviews WHERE tenant_id=p_tenant AND id=s.review_id FOR UPDATE;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>4
  OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
   AND (state<>'succeeded' OR cancel_requested OR operation<>'media.preview'
    OR operation_version<>'1'))
  OR (SELECT count(*) FROM photo.backend_documents WHERE tenant_id=p_tenant
   AND run_id=p_run AND purpose='task_params')<>4 THEN RETURN false;END IF;
 FOREACH side IN ARRAY ARRAY['before','after'] LOOP
  FOREACH variant IN ARRAY ARRAY['preview1600','preview2560'] LOOP
   SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.tasks t
    ON t.tenant_id=b.tenant_id AND t.id=b.owner_id AND t.run_id=b.run_id
    AND t.params_artifact_id=b.artifact_id
   WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.purpose='task_params'
    AND t.node_key='restoration.'||side||':'||variant FOR UPDATE OF b;
   IF NOT FOUND OR d.state<>'ready' OR d.v28_restoration_preview_params IS NULL
    OR d.v28_restoration_preview_derivative_receipt IS NULL THEN RETURN false;END IF;
   receipt:=d.v28_restoration_preview_derivative_receipt;
   IF NOT photo.record_v28_restoration_preview_derivative_receipt(
    p_tenant,p_run,side,variant,(receipt->>'result_id')::uuid,
    (receipt->>'attempt')::integer,(receipt->>'fence')::bigint,
    receipt->>'manifest',receipt->'locations') THEN RETURN false;END IF;
  END LOOP;
 END LOOP;
 UPDATE photo.runs SET status='completed',revision=revision+1
  WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_successful_v28_restoration_preview(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_successful_v28_restoration_preview(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.finalize_successful_v28_restoration_preview(uuid,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
