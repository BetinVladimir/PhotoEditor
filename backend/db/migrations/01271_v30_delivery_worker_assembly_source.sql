-- +goose Up
-- Preserve the existing tenant-context wrapper and worker/resource fencing.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_delivery_source_ready_before_tenant_context(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE operation text;accepted boolean;
BEGIN
 IF photo.v30_delivery_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT t.operation INTO operation FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=p_task;
 IF operation='v2.delivery.assemble' THEN
  accepted:=photo.v30_delivery_assembly_source_ready(p_tenant,p_run,p_task,p_params,p_sha,p_json);
 ELSIF operation='v2.delivery.prepare' THEN
  accepted:=photo.v30_delivery_prepare_source_ready(p_tenant,p_run,p_task,p_params,p_sha,p_json);
 ELSE RETURN false;END IF;
 IF accepted IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN photo.v30_delivery_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest);
END $$;
ALTER FUNCTION photo.v30_delivery_source_ready_before_tenant_context(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_source_ready_before_tenant_context(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
