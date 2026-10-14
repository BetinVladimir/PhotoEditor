-- +goose Up
-- Generic graph admission must not activate identity.compare without its
-- exact live consent pin. Keep the previous graph gates in the inner function.
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) RENAME TO seal_v2_graph_pre_v23_identity;
REVOKE ALL ON FUNCTION photo.seal_v2_graph_pre_v23_identity(uuid,uuid)
 FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
  AND operation='identity.compare') THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_pre_v23_identity(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v23_identity_compare(p_tenant uuid,p_run uuid,
 p_workflow uuid,p_task uuid,p_consents text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_workflow IS NULL OR p_task IS NULL
  THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1 OR
  NOT EXISTS(SELECT 1 FROM photo.v2_workflows w JOIN photo.tasks t
   ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id
   WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.id=p_workflow
    AND t.id=p_task AND t.operation='identity.compare'
    AND t.operation_version='1' AND t.resource_class='gpu')
   THEN RETURN false;END IF;
 IF photo.pin_v23_identity_compare_consents(p_tenant,p_workflow,p_task,p_consents)
   IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_pre_v23_identity(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.activate_v23_identity_compare(uuid,uuid,uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v23_identity_compare(uuid,uuid,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.activate_v23_identity_compare(uuid,uuid,uuid,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
