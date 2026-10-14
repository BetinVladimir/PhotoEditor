-- +goose Up
-- A missing reservation sample cannot turn an unfinished running or retrying
-- task into evidence for a 900-second idle scale-down window.
-- +goose StatementBegin
DO $$ BEGIN
 IF to_regprocedure('photo.capacity_pool_pending_backlog(text)') IS NULL THEN
  RAISE EXCEPTION 'CAPACITY_BACKLOG_CONTRACT_MISSING';
 END IF;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.capacity_pool_pending_backlog(p_pool text) RETURNS bigint
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE total bigint;
BEGIN
 IF p_pool IS NULL OR p_pool NOT IN('cpu','gpu','signer') THEN
  RAISE EXCEPTION 'INVALID_CAPACITY_POOL' USING ERRCODE='22023';
 END IF;
 SELECT count(*) INTO total FROM photo.tasks t
 JOIN photo.runs r ON r.id=t.run_id AND r.tenant_id=t.tenant_id
 WHERE r.kind='v2' AND r.config_snapshot->>'queue_class' IN('interactive','capture','batch','training')
 AND t.state IN('ready','queued','running','retry_wait')
 AND CASE WHEN p_pool='signer' THEN t.execution_profile->>'execution_pool'='signer'
  WHEN p_pool='gpu' THEN t.resource_class='gpu'
   AND coalesce(t.execution_profile->>'execution_pool','')<>'signer'
  ELSE t.resource_class<>'gpu'
   AND coalesce(t.execution_profile->>'execution_pool','')<>'signer' END;
 RETURN total;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
