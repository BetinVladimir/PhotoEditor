-- +goose Up
-- Pure exact batch projection consistency. Ready artifacts, private review
-- proof, qualification, resources and actual graph registration remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_exposure_inventory_batch_ready(p_nodes jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE inventory_row jsonb; node_row jsonb; transport_row jsonb; roots jsonb; n integer;
BEGIN
 IF jsonb_typeof(p_nodes) IS DISTINCT FROM 'array' OR jsonb_typeof(p_transport) IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_inventory) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 n:=jsonb_array_length(p_nodes);
 IF n NOT BETWEEN 1 AND 100 OR jsonb_array_length(p_transport)<>n OR jsonb_array_length(p_inventory)<>n
 OR (SELECT count(DISTINCT r->>'id') FROM jsonb_array_elements(p_nodes) r)<>n
 OR (SELECT count(DISTINCT r->>'task_id') FROM jsonb_array_elements(p_transport) r)<>n
 OR (SELECT count(DISTINCT r->>'task_id') FROM jsonb_array_elements(p_inventory) r)<>n THEN RETURN false;END IF;
 FOR inventory_row IN SELECT value FROM jsonb_array_elements(p_inventory) LOOP
  IF jsonb_typeof(inventory_row) IS DISTINCT FROM 'object' OR NOT(inventory_row ?& ARRAY['task_id','entries'])
  OR (inventory_row-'task_id'-'entries')<>'{}'::jsonb OR jsonb_typeof(inventory_row->'task_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(inventory_row->'entries') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  IF jsonb_array_length(inventory_row->'entries') NOT BETWEEN 1 AND 127 THEN RETURN false;END IF;
  SELECT value INTO node_row FROM jsonb_array_elements(p_nodes) WHERE value->>'id'=inventory_row->>'task_id';
  SELECT value INTO transport_row FROM jsonb_array_elements(p_transport) WHERE value->>'task_id'=inventory_row->>'task_id';
  IF node_row IS NULL OR transport_row IS NULL OR node_row->>'operation' IS DISTINCT FROM 'semantic.local_exposure'
  OR node_row->>'operation_version' IS DISTINCT FROM '1'
  OR jsonb_typeof(transport_row->'entry_count') IS DISTINCT FROM 'number'
  OR (transport_row->>'entry_count') !~ '^[1-9][0-9]*$'
  OR (transport_row->>'entry_count')::numeric<>jsonb_array_length(inventory_row->'entries') THEN RETURN false;END IF;
  SELECT jsonb_agg(e.value->'ref' ORDER BY e.ordinality) INTO roots
   FROM jsonb_array_elements(inventory_row->'entries') WITH ORDINALITY e(value,ordinality);
  IF roots IS DISTINCT FROM transport_row->'direct_roots' THEN RETURN false;END IF;
  IF NOT photo.semantic_exposure_inventory_digest_ready((inventory_row->>'task_id')::uuid,
   (node_row->>'params_artifact_id')::uuid,node_row->>'params_sha',node_row->>'input_digest',
   transport_row->>'entries_sha',inventory_row->'entries') THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_exposure_inventory_batch_ready(jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_exposure_inventory_batch_ready(jsonb,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
