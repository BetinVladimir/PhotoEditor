-- +goose Up
-- Scheduler pool calls need tenant-local context for nested V26 predicates.
-- Existing lease/source checks remain authoritative; restore context on all exits.
-- +goose StatementBegin
ALTER FUNCTION photo.v30_delivery_prepare_source_ready(uuid,uuid,uuid,uuid,text,text) RENAME TO v30_delivery_prepare_source_ready_before_tenant_context;
REVOKE ALL ON FUNCTION photo.v30_delivery_prepare_source_ready_before_tenant_context(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
CREATE FUNCTION photo.v30_delivery_prepare_source_ready(p_tenant uuid,p_run uuid,p_task uuid,p_params uuid,p_sha text,p_json text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE previous text;accepted boolean;
BEGIN
 IF p_tenant IS NULL THEN RETURN false;END IF;
 previous:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 BEGIN
  accepted:=photo.v30_delivery_prepare_source_ready_before_tenant_context(p_tenant,p_run,p_task,p_params,p_sha,p_json);
 EXCEPTION WHEN OTHERS THEN
  PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
  RAISE;
 END;
 PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
 RETURN accepted;
END $$;
ALTER FUNCTION photo.v30_delivery_prepare_source_ready(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_prepare_source_ready(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
ALTER FUNCTION photo.v30_delivery_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) RENAME TO v30_delivery_source_ready_before_tenant_context;
REVOKE ALL ON FUNCTION photo.v30_delivery_source_ready_before_tenant_context(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
CREATE FUNCTION photo.v30_delivery_source_ready(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE previous text;accepted boolean;
BEGIN
 IF p_tenant IS NULL THEN RETURN false;END IF;
 previous:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 BEGIN
  accepted:=photo.v30_delivery_source_ready_before_tenant_context(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest);
 EXCEPTION WHEN OTHERS THEN
  PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
  RAISE;
 END;
 PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
 RETURN accepted;
END $$;
ALTER FUNCTION photo.v30_delivery_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
