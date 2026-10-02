-- Every generated WorkingImage receives a display derivative before the run can finish.
-- +goose Up
ALTER TABLE photo.interactive_tasks DROP CONSTRAINT interactive_tasks_tenant_id_run_id_ordinal_key;
ALTER TABLE photo.interactive_tasks ADD CONSTRAINT interactive_tasks_tenant_run_kind_ordinal_key UNIQUE(tenant_id,run_id,kind,ordinal);
ALTER TABLE photo.interactive_tasks ADD COLUMN promotion_token uuid;
ALTER TABLE photo.interactive_tasks ADD COLUMN promotion_until timestamptz;
ALTER TABLE photo.interactive_tasks ADD CHECK((promotion_token IS NULL)=(promotion_until IS NULL));
ALTER TABLE photo.interactive_tasks DROP CONSTRAINT interactive_tasks_kind_check;
ALTER TABLE photo.interactive_tasks DROP CONSTRAINT interactive_tasks_operation_check;
ALTER TABLE photo.interactive_tasks ADD CONSTRAINT interactive_tasks_kind_check
 CHECK(kind IN('mask_manual','mask_prompt','generation','generation_preview','preview'));
ALTER TABLE photo.interactive_tasks ADD CONSTRAINT interactive_tasks_operation_check
 CHECK(operation=CASE kind WHEN 'mask_manual' THEN 'mask.rasterize' WHEN 'mask_prompt' THEN 'mask.prompt' WHEN 'generation' THEN 'image.generate' ELSE 'editor.preview' END);

-- +goose StatementBegin
CREATE FUNCTION photo.claim_generation_preview()
RETURNS TABLE(tenant_id uuid,run_id uuid,source_task_id uuid,ordinal int,artifact_id uuid,sha256 text,token uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE picked record; fence uuid:=gen_random_uuid();
BEGIN
 SELECT j.tenant_id,j.run_id,j.task_id,j.ordinal,a.id artifact_id,a.sha256 INTO picked
 FROM photo.interactive_tasks j
 JOIN photo.tasks t ON t.tenant_id=j.tenant_id AND t.id=j.task_id AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
  AND a.output_role='working_image' AND a.state='ready' AND a.object_version IS NOT NULL
  AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
 WHERE j.kind='generation' AND j.state='complete' AND (j.promotion_until IS NULL OR j.promotion_until<clock_timestamp())
 AND NOT EXISTS(SELECT FROM photo.interactive_tasks p WHERE p.tenant_id=j.tenant_id AND p.run_id=j.run_id AND p.kind='generation_preview' AND p.ordinal=j.ordinal)
 ORDER BY j.run_id,j.ordinal LIMIT 1 FOR UPDATE OF j SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.interactive_tasks SET promotion_token=fence,promotion_until=clock_timestamp()+interval '60 seconds' WHERE task_id=picked.task_id;
 RETURN QUERY SELECT picked.tenant_id,picked.run_id,picked.task_id,picked.ordinal,picked.artifact_id,picked.sha256,fence;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_generation_preview() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_generation_preview() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_generation_preview() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.register_generation_preview(p_source uuid,p_token uuid,p_task uuid,p_params jsonb,p_sha text,p_inputs jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.interactive_tasks%ROWTYPE; target record; n int;
BEGIN
 IF p_task IS NULL OR p_params IS NULL OR jsonb_typeof(p_params)<>'object' OR p_sha!~'^[0-9a-f]{64}$'
 OR p_inputs IS NULL OR jsonb_typeof(p_inputs)<>'array' OR jsonb_array_length(p_inputs)<>1 THEN RETURN false;END IF;
 SELECT * INTO j FROM photo.interactive_tasks WHERE task_id=p_source FOR UPDATE;
 IF NOT FOUND OR j.kind<>'generation' OR j.promotion_token IS DISTINCT FROM p_token OR j.promotion_until<clock_timestamp() THEN RETURN false;END IF;
 SELECT a.id,a.sha256 INTO target FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token AND a.output_role='working_image' AND a.state='ready' AND a.object_version IS NOT NULL
 WHERE t.tenant_id=j.tenant_id AND t.id=j.task_id AND t.state='succeeded' AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256));
 IF target.id IS NULL OR p_params->'image'<>jsonb_build_object('artifact_id',target.id,'sha256',target.sha256)
 OR p_inputs<>jsonb_build_array(jsonb_build_object('artifact_id',target.id,'sha256',target.sha256))
 OR p_params->>'scale'<>'proxy' OR p_params->'modules'<>jsonb_build_object('strength',1) THEN RETURN false;END IF;
 INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,ordinal,operation,source_artifact_id,declared_inputs,params,params_sha256,backend)
 VALUES(j.tenant_id,j.run_id,p_task,'generation_preview',j.ordinal,'editor.preview',target.id,p_inputs,p_params,p_sha,'cpu') ON CONFLICT DO NOTHING;
 GET DIAGNOSTICS n=ROW_COUNT;
 UPDATE photo.interactive_tasks SET promotion_token=NULL,promotion_until=NULL WHERE task_id=p_source AND promotion_token=p_token;
 RETURN n=1 OR EXISTS(SELECT FROM photo.interactive_tasks p WHERE p.tenant_id=j.tenant_id AND p.run_id=j.run_id AND p.kind='generation_preview' AND p.ordinal=j.ordinal AND p.params_sha256=p_sha);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_generation_preview(uuid,uuid,uuid,jsonb,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_generation_preview(uuid,uuid,uuid,jsonb,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_generation_preview(uuid,uuid,uuid,jsonb,text,jsonb) TO photo_scheduler;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finalize_interactive_run() RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE picked record;target text;
BEGIN
 SELECT r.tenant_id,r.id,
  EXISTS(SELECT FROM photo.interactive_tasks j JOIN photo.tasks t ON t.tenant_id=j.tenant_id AND t.id=j.task_id WHERE j.tenant_id=r.tenant_id AND j.run_id=r.id AND t.state<>'succeeded') failed,
  EXISTS(SELECT FROM photo.interactive_tasks j JOIN photo.tasks t ON t.tenant_id=j.tenant_id AND t.id=j.task_id JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id WHERE j.tenant_id=r.tenant_id AND j.run_id=r.id AND z.review_required) review INTO picked
 FROM photo.runs r
 WHERE r.graph_sealed AND r.status='running'
 AND EXISTS(SELECT FROM photo.interactive_tasks j WHERE j.tenant_id=r.tenant_id AND j.run_id=r.id)
 AND NOT EXISTS(SELECT FROM photo.interactive_tasks pending LEFT JOIN photo.tasks pt ON pt.tenant_id=pending.tenant_id AND pt.id=pending.task_id WHERE pending.tenant_id=r.tenant_id AND pending.run_id=r.id AND (pt.id IS NULL OR pt.state NOT IN('succeeded','failed','cancelled','skipped')))
 AND NOT EXISTS(SELECT FROM photo.interactive_tasks g WHERE g.tenant_id=r.tenant_id AND g.run_id=r.id AND g.kind='generation' AND NOT EXISTS(SELECT FROM photo.interactive_tasks p WHERE p.tenant_id=g.tenant_id AND p.run_id=g.run_id AND p.kind='generation_preview' AND p.ordinal=g.ordinal))
 ORDER BY r.id LIMIT 1 FOR UPDATE OF r SKIP LOCKED;
 IF NOT FOUND THEN RETURN false;END IF;
 target:=CASE WHEN picked.failed THEN 'failed' WHEN picked.review THEN 'waiting_review' ELSE 'completed' END;
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
