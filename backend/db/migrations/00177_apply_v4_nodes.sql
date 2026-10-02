-- Plan-bound node registration for every Apply v4 worker stage.
-- +goose Up
ALTER TABLE photo.apply_nodes DROP CONSTRAINT apply_nodes_phase_check;
ALTER TABLE photo.apply_nodes ADD CONSTRAINT apply_nodes_phase_check CHECK(phase ~ '^[a-z][a-z0-9_]{0,63}$');
ALTER TABLE photo.apply_nodes DROP CONSTRAINT apply_nodes_operation_check;
ALTER TABLE photo.apply_nodes ADD CONSTRAINT apply_nodes_operation_check CHECK(operation ~ '^[a-z][a-z0-9_.]{0,119}$');
GRANT SELECT,INSERT ON photo.apply_nodes TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.register_apply_v4_node(p_tenant uuid,p_run uuid,p_token uuid,p_node jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;s photo.apply_v4_stages%ROWTYPE;old photo.apply_nodes%ROWTYPE;entry jsonb;expected_ordinal int;source_ordinal int;
BEGIN
 IF p_node IS NULL OR jsonb_typeof(p_node)<>'object' OR octet_length(p_node::text)>1048576
 OR (p_node-'id'-'phase'-'ordinal'-'source_item_id'-'source_asset_id'-'source_version_id'-'node_key'-'operation'-'inputs'-'prerequisites'-'params_sha256'-'params')<>'{}'
 OR p_node->>'params_sha256' !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_node->'inputs')<>'array' OR jsonb_array_length(p_node->'inputs') NOT BETWEEN 1 AND 128
 OR jsonb_typeof(p_node->'prerequisites')<>'array' OR jsonb_typeof(p_node->'params')<>'object' THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR b.graph_version<>4 OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() OR b.stage_index IS NULL THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND stage_index=b.stage_index;
 IF NOT FOUND OR s.phase<>b.phase OR s.cardinality='review' OR p_node->>'phase'<>s.phase OR p_node->>'operation'<>s.operation THEN RETURN false;END IF;
 expected_ordinal:=(p_node->>'ordinal')::int;
 IF (s.cardinality='item' AND expected_ordinal NOT BETWEEN 0 AND b.item_count-1) OR (s.cardinality='aggregate' AND expected_ordinal<>0) THEN RETURN false;END IF;
 source_ordinal:=CASE WHEN s.cardinality='aggregate' THEN 0 ELSE expected_ordinal END;
 IF NOT EXISTS(SELECT FROM photo.runs r JOIN photo.snapshot_items i ON i.tenant_id=r.tenant_id AND i.snapshot_id=r.input_snapshot_id
  JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND i.ordinal=source_ordinal AND i.item_id=(p_node->>'source_item_id')::uuid
   AND v.asset_id=(p_node->>'source_asset_id')::uuid AND v.id=(p_node->>'source_version_id')::uuid) THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_node->'inputs') LOOP
  IF (entry-'artifact_id'-'sha256')<>'{}' OR entry->>'sha256' !~ '^[0-9a-f]{64}$'
   OR NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(entry->>'artifact_id')::uuid AND a.sha256=entry->>'sha256' AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 END LOOP;
 IF EXISTS(SELECT FROM jsonb_array_elements_text(p_node->'prerequisites') x
  LEFT JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=x::uuid AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL
  LEFT JOIN photo.apply_nodes predecessor ON predecessor.tenant_id=t.tenant_id AND predecessor.id=t.id
  LEFT JOIN photo.apply_v4_stages ps ON ps.tenant_id=predecessor.tenant_id AND ps.run_id=predecessor.run_id AND ps.phase=predecessor.phase
  WHERE t.id IS NULL OR ps.stage_index>=s.stage_index) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_node->>'id')::uuid;
 IF FOUND THEN RETURN to_jsonb(old)-'tenant_id'-'run_id'=p_node;END IF;
 IF EXISTS(SELECT FROM photo.apply_nodes WHERE id=(p_node->>'id')::uuid) THEN RETURN false;END IF;
 INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,source_asset_id,source_version_id,node_key,operation,inputs,prerequisites,params_sha256,params)
 VALUES((p_node->>'id')::uuid,p_tenant,p_run,s.phase,expected_ordinal,(p_node->>'source_item_id')::uuid,(p_node->>'source_asset_id')::uuid,(p_node->>'source_version_id')::uuid,p_node->>'node_key',s.operation,p_node->'inputs',ARRAY(SELECT jsonb_array_elements_text(p_node->'prerequisites')::uuid),p_node->>'params_sha256',p_node->'params');
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR unique_violation THEN RETURN false;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_apply_v4_node(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_apply_v4_node(uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_apply_v4_node(uuid,uuid,uuid,jsonb) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
