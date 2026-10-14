-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_worker_cache_context(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS TABLE(artifact_id uuid,sha256 text,execution_profile jsonb,parent_roots jsonb,qualification jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT photo.admit_v26_render_recipe(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) THEN RETURN;END IF;
 RETURN QUERY SELECT t.params_artifact_id,p_sha,t.execution_profile,
 (SELECT coalesce(jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id::text,'sha256',i.sha256)
 ORDER BY i.ordinal,i.artifact_id),'[]'::jsonb) FROM photo.v2_task_inputs i
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='ImageResult'),r.v26_qualification_pin
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task;
END $$;
CREATE FUNCTION photo.lookup_v26_render_worker_cache(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text,p_key text,p_body text)
RETURNS TABLE(result_id uuid,root jsonb,inventory jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT photo.admit_v26_render_recipe(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) THEN RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.lookup_v26_render_cache(p_tenant,p_task,p_attempt,p_fence,p_key,p_body);
END $$;
ALTER FUNCTION photo.v26_render_worker_cache_context(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.lookup_v26_render_worker_cache(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_worker_cache_context(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION photo.lookup_v26_render_worker_cache(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_worker_cache_context(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
GRANT EXECUTE ON FUNCTION photo.lookup_v26_render_worker_cache(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
