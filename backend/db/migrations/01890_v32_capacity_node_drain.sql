-- +goose Up
-- Keep drain authority on the same row that reserve_dispatch_resources locks.
-- Existing leases may finish; a separate fenced recovery path handles timeouts.
ALTER TABLE photo.resource_nodes
 ADD COLUMN drain_generation bigint NOT NULL DEFAULT 0 CHECK(drain_generation>=0),
 ADD COLUMN drain_started_at timestamptz;

-- +goose StatementBegin
CREATE FUNCTION photo.begin_capacity_node_drain(p_node text) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n photo.resource_nodes%ROWTYPE;
BEGIN
 IF p_node IS NULL OR length(p_node)=0 THEN
  RAISE EXCEPTION 'INVALID_NODE' USING ERRCODE='22023';
 END IF;
 SELECT * INTO n FROM photo.resource_nodes WHERE node_id=p_node FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'UNKNOWN_NODE' USING ERRCODE='22023'; END IF;
 IF n.drain_started_at IS NOT NULL AND NOT n.enabled THEN
  RETURN n.drain_generation;
 END IF;
 UPDATE photo.resource_nodes SET enabled=false,
  drain_generation=drain_generation+1,drain_started_at=clock_timestamp()
 WHERE node_id=p_node RETURNING drain_generation INTO n.drain_generation;
 RETURN n.drain_generation;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_capacity_node_drain(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_capacity_node_drain(text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.begin_capacity_node_drain(text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.capacity_node_drain_status(p_node text,p_generation bigint)
RETURNS TABLE(active_reservations bigint,occupied_slots bigint,ready_to_stop boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n photo.resource_nodes%ROWTYPE;
BEGIN
 IF p_node IS NULL OR p_generation IS NULL OR p_generation<1 THEN
  RAISE EXCEPTION 'INVALID_DRAIN_PROOF' USING ERRCODE='22023';
 END IF;
 SELECT * INTO n FROM photo.resource_nodes WHERE node_id=p_node FOR UPDATE;
 IF NOT FOUND OR n.drain_generation<>p_generation OR n.drain_started_at IS NULL OR n.enabled THEN
  RAISE EXCEPTION 'STALE_DRAIN_PROOF' USING ERRCODE='22023';
 END IF;
 RETURN QUERY SELECT
  (SELECT count(*) FROM photo.resource_reservations r WHERE r.node_id=p_node AND r.state='reserved'),
  (SELECT count(*) FROM photo.resource_slots s WHERE s.node_id=p_node AND s.task_id IS NOT NULL),
  NOT EXISTS(SELECT 1 FROM photo.resource_reservations r WHERE r.node_id=p_node AND r.state='reserved')
  AND NOT EXISTS(SELECT 1 FROM photo.resource_slots s WHERE s.node_id=p_node AND s.task_id IS NOT NULL);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_node_drain_status(text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_node_drain_status(text,bigint) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.capacity_node_drain_status(text,bigint) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
