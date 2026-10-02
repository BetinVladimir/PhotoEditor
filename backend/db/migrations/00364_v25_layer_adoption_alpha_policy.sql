-- +goose Up
-- Read only the alpha policy of an exact result generation and frozen node.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_adoption_alpha_policy(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS text LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node jsonb; nodes jsonb; profile jsonb; matches integer; mode text;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM photo.v25_layer_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result)) THEN RETURN NULL; END IF;
 SELECT r.config_snapshot->'v25_composition'->'nodes',
   t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha'
 INTO nodes,profile FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task;
 IF jsonb_typeof(nodes) IS DISTINCT FROM 'array' THEN RETURN NULL; END IF;
 SELECT count(*) INTO matches FROM jsonb_array_elements(nodes) n WHERE n->>'task_id'=p_task::text;
 IF matches<>1 THEN RETURN NULL; END IF;
 SELECT n INTO node FROM jsonb_array_elements(nodes) n WHERE n->>'task_id'=p_task::text;
 IF node->'profile' IS DISTINCT FROM profile OR NOT photo.v25_layer_profile_shape(
  (node->>'params_jcs')::jsonb,(node->>'width')::bigint,(node->>'height')::bigint,profile) THEN RETURN NULL; END IF;
 mode:=coalesce(profile->>'layer_alpha_mode','require_float32');
 IF mode NOT IN ('require_float32','normalize_uint16') THEN RETURN NULL; END IF;
 RETURN mode;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_adoption_alpha_policy(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_adoption_alpha_policy(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_adoption_alpha_policy(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
