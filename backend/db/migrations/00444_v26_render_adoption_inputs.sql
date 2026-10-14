-- +goose Up
-- Task-generation-bound recipe/profile/parent reads for independent replay.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_adoption_inputs(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,execution_profile jsonb,parent_roots jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; a photo.artifacts%ROWTYPE; parents jsonb;
BEGIN
 IF p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 OR p_result IS NULL THEN RETURN;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.id IS NULL OR t.operation<>'render.compose' OR t.operation_version<>'1'
  OR t.state<>'succeeded' OR t.cancel_requested OR t.attempt IS DISTINCT FROM p_attempt
  OR t.fencing_token IS DISTINCT FROM p_fence OR t.canonical_result_id IS DISTINCT FROM p_result
  OR NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.id=p_result
   AND z.task_id=p_task AND z.attempt=p_attempt AND z.fencing_token=p_fence)
  OR NOT EXISTS(SELECT 1 FROM photo.v2_adoption_context(p_tenant,p_task))
  OR NOT photo.v26_render_live_qualification_ready(p_tenant,t.run_id)
  OR NOT photo.v26_render_adoption_inputs_ready(p_tenant,p_task) THEN RETURN;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=t.params_artifact_id;
 IF a.id IS NULL OR a.media_type<>'application/json' OR a.output_role<>'document'
  OR a.bytes NOT BETWEEN 2 AND 1048576 THEN RETURN;END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id::text,'sha256',i.sha256)
  ORDER BY i.ordinal,i.artifact_id),'[]'::jsonb) INTO parents
  FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='ImageResult';
 IF jsonb_array_length(parents)>33 THEN RETURN;END IF;
 RETURN QUERY SELECT a.id,a.sha256,t.execution_profile,parents;
END $$;
ALTER FUNCTION photo.v26_render_adoption_inputs(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_adoption_inputs(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_adoption_inputs(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
