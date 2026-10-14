-- +goose Up
-- Generation-scoped immutable complete qualification identity for replay.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_adoption_qualification(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM photo.v26_render_adoption_inputs(p_tenant,p_task,p_attempt,p_fence,p_result)) THEN RETURN NULL;END IF;
 SELECT r.v26_qualification_pin INTO pin FROM photo.tasks t JOIN photo.runs r
 ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.tenant_id=p_tenant AND t.id=p_task
 AND t.attempt=p_attempt AND t.fencing_token=p_fence AND t.canonical_result_id=p_result;
 RETURN pin;
END $$;
ALTER FUNCTION photo.v26_render_adoption_qualification(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_adoption_qualification(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_adoption_qualification(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
