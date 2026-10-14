-- +goose Up
-- Include unavailable-model and temporarily delayed work in the idle veto.
-- This aggregate never exposes tenant/task identifiers to the operator.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_pool_pending_backlog(p_pool text) RETURNS bigint
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE total bigint;
BEGIN
 IF p_pool IS NULL OR p_pool NOT IN('cpu','gpu','signer') THEN
  RAISE EXCEPTION 'INVALID_CAPACITY_POOL' USING ERRCODE='22023';
 END IF;
 SELECT count(*) INTO total FROM photo.tasks t
 JOIN photo.runs r ON r.id=t.run_id AND r.tenant_id=t.tenant_id
 JOIN photo.tenants owner ON owner.id=t.tenant_id AND owner.status='active'
 WHERE r.kind='v2' AND r.config_snapshot->>'queue_class' IN('interactive','capture','batch','training')
 AND t.state IN('ready','queued') AND t.attempt<6 AND NOT t.cancel_requested
 AND NOT r.cancel_requested AND r.graph_sealed AND r.status IN('queued','running')
 AND CASE WHEN p_pool='signer' THEN t.execution_profile->>'execution_pool'='signer'
  WHEN p_pool='gpu' THEN t.resource_class='gpu'
   AND coalesce(t.execution_profile->>'execution_pool','')<>'signer'
  ELSE t.resource_class<>'gpu'
   AND coalesce(t.execution_profile->>'execution_pool','')<>'signer' END;
 RETURN total;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_pool_pending_backlog(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_pool_pending_backlog(text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.capacity_pool_pending_backlog(text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
