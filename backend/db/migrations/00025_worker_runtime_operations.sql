-- +goose Up
-- A runtime may not consume a queue if provisioned operations exceed its build.
-- +goose StatementBegin
CREATE FUNCTION photo.worker_runtime_operations(p_worker uuid,p_class text)
RETURNS TABLE(operation text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT DISTINCT op.operation
 FROM photo.worker_registrations w JOIN photo.resource_nodes n ON n.node_id=w.node_id
 JOIN photo.operation_registry op ON op.resource_class=p_class AND op.version='1'
 WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled AND n.enabled
 AND w.resource_classes ? p_class AND w.backends ? 'cpu' AND (w.qualified_operations->op.operation) ? 'cpu'
 AND w.certificate_sha256 IS NOT NULL
 ORDER BY op.operation LIMIT 129;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.worker_runtime_operations(uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.worker_runtime_operations(uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.worker_runtime_operations(uuid,text) TO photo_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
