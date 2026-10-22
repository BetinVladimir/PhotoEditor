-- +goose Up
-- Failed variants cannot produce display previews. Successful variants still must.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finalize_interactive_run() RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE picked record;target text;
BEGIN
 SELECT r.tenant_id,r.id,
  EXISTS(SELECT FROM photo.interactive_tasks j JOIN photo.tasks t ON t.tenant_id=j.tenant_id AND t.id=j.task_id WHERE j.tenant_id=r.tenant_id AND j.run_id=r.id AND t.state<>'succeeded') failed,
  EXISTS(SELECT FROM photo.interactive_tasks j JOIN photo.tasks t ON t.tenant_id=j.tenant_id AND t.id=j.task_id JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id WHERE j.tenant_id=r.tenant_id AND j.run_id=r.id AND z.review_required) review,
  EXISTS(SELECT FROM photo.interactive_tasks p JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.task_id AND t.state='succeeded' AND NOT t.cancel_requested
   JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
   JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
    AND a.output_role='derivative' AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
    AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
   WHERE p.tenant_id=r.tenant_id AND p.run_id=r.id AND p.kind='generation_preview') usable INTO picked
 FROM photo.runs r
 WHERE r.graph_sealed AND r.status='running'
 AND EXISTS(SELECT FROM photo.interactive_tasks j WHERE j.tenant_id=r.tenant_id AND j.run_id=r.id)
 AND NOT EXISTS(SELECT FROM photo.interactive_tasks pending LEFT JOIN photo.tasks pt ON pt.tenant_id=pending.tenant_id AND pt.id=pending.task_id WHERE pending.tenant_id=r.tenant_id AND pending.run_id=r.id AND (pt.id IS NULL OR pt.state NOT IN('succeeded','failed','cancelled','skipped')))
 AND NOT EXISTS(SELECT FROM photo.interactive_tasks g JOIN photo.tasks gt ON gt.tenant_id=g.tenant_id AND gt.id=g.task_id AND gt.state='succeeded' WHERE g.tenant_id=r.tenant_id AND g.run_id=r.id AND g.kind='generation' AND NOT EXISTS(SELECT FROM photo.interactive_tasks p WHERE p.tenant_id=g.tenant_id AND p.run_id=g.run_id AND p.kind='generation_preview' AND p.ordinal=g.ordinal))
 ORDER BY r.id LIMIT 1 FOR UPDATE OF r SKIP LOCKED;
 IF NOT FOUND THEN RETURN false;END IF;
 target:=CASE WHEN picked.failed AND picked.usable THEN 'completed_with_errors' WHEN picked.failed THEN 'failed' WHEN picked.review THEN 'waiting_review' ELSE 'completed' END;
 UPDATE photo.runs SET status=target,revision=revision+1 WHERE tenant_id=picked.tenant_id AND id=picked.id AND status='running';
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_interactive_run() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_interactive_run() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_interactive_run() TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
