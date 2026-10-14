-- +goose Up
-- Worker identity is exposed only after full live lease/recipe admission.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_worker_qualification(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb;
BEGIN
 IF NOT photo.admit_v26_render_recipe(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) THEN RETURN NULL;END IF;
 SELECT v26_qualification_pin INTO pin FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 RETURN pin;
END $$;
ALTER FUNCTION photo.v26_render_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
