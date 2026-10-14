-- +goose Up
-- Private persisted-row verification. Final admission retains scope/artifact
-- locks and separately proves source, qualification, quota and ready transport.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_apply_registered_inventory_ready(p_tenant uuid,p_project uuid,p_child uuid,p_nodes jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected_count integer; actual_count integer;
BEGIN
 IF NOT photo.semantic_exposure_inventory_batch_ready(p_nodes,p_transport,p_inventory) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child AND kind='apply'
 AND config_snapshot->>'project_id'=p_project::text AND NOT cancel_requested) THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child)<>jsonb_array_length(p_nodes)
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(p_nodes) n WHERE NOT EXISTS(
 SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_child AND t.id::text=n->>'id'
 AND t.operation='semantic.local_exposure' AND t.operation_version='1' AND t.resource_class='cpu'
 AND t.params_artifact_id::text=n->>'params_artifact_id' AND t.input_digest=n->>'input_digest' AND NOT t.cancel_requested)) THEN RETURN false;END IF;
 SELECT sum(jsonb_array_length(i->'entries'))::integer INTO expected_count FROM jsonb_array_elements(p_inventory) i;
 SELECT expected_count+sum(jsonb_array_length(t->'chunks')+1)::integer INTO expected_count FROM jsonb_array_elements(p_transport) t;
 SELECT count(*) INTO actual_count FROM photo.v2_task_inputs i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id
 WHERE t.tenant_id=p_tenant AND t.run_id=p_child;
 IF actual_count<>expected_count THEN RETURN false;END IF;
 RETURN NOT EXISTS(
 WITH expected AS (
 SELECT (i->>'task_id')::uuid AS task_id,(e.value->'ref'->>'artifact_id')::uuid AS artifact_id,e.value->'ref'->>'sha256' AS sha,e.value->>'contract' AS contract,(e.ordinality-1)::integer AS ordinal
 FROM jsonb_array_elements(p_inventory) i CROSS JOIN LATERAL jsonb_array_elements(i->'entries') WITH ORDINALITY e(value,ordinality)
 UNION ALL
 SELECT (t->>'task_id')::uuid,(e.value->>'artifact_id')::uuid,e.value->>'sha256','InputClosureChunk',1000000+(e.ordinality-1)::integer
 FROM jsonb_array_elements(p_transport) t CROSS JOIN LATERAL jsonb_array_elements(t->'chunks') WITH ORDINALITY e(value,ordinality)
 UNION ALL
 SELECT (t->>'task_id')::uuid,(t->'manifest'->>'artifact_id')::uuid,t->'manifest'->>'sha256','InputClosureManifest',1000000+jsonb_array_length(t->'chunks')
 FROM jsonb_array_elements(p_transport) t)
 SELECT 1 FROM expected e WHERE NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
 WHERE i.tenant_id=p_tenant AND i.project_id=p_project AND i.task_id=e.task_id AND i.artifact_id=e.artifact_id
 AND i.sha256=e.sha AND i.contract=e.contract AND i.ordinal=e.ordinal));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_apply_registered_inventory_ready(uuid,uuid,uuid,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_apply_registered_inventory_ready(uuid,uuid,uuid,jsonb,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
