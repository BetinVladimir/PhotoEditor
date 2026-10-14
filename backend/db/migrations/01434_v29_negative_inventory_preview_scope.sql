-- +goose Up
-- Include external execution-only consumers and retain the exact canonical
-- negative preview exception. Forward-only replacement of the inventory reader.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.read_v29_negative_dependency_inventory(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_origin uuid,p_max_tasks integer,p_max_edges integer)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ids uuid[];parents jsonb;result jsonb;edge_count integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_max_tasks IS NULL OR p_max_edges IS NULL OR p_max_tasks NOT BETWEEN 1 AND 100000 OR p_max_edges NOT BETWEEN 0 AND 1000000
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 WHERE t.tenant_id=p_tenant AND t.id=p_origin AND w.project_id=p_project AND t.operation='optical.negative' AND t.operation_version='1') THEN RETURN NULL;END IF;
 SELECT array_agg(id ORDER BY id) INTO ids FROM (
 SELECT t.id FROM photo.tasks t JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 WHERE t.tenant_id=p_tenant AND w.project_id=p_project ORDER BY t.id LIMIT p_max_tasks+1) selected;
 IF cardinality(ids)>p_max_tasks OR cardinality(ids)<1 THEN RETURN NULL;END IF;
 -- Combine execution dependencies and exact artifact reads. UNION deduplicates
 -- the same parent consumed through direct roots and registered closure rows.
 WITH dependency AS (
 SELECT e.dependent_id child,e.prerequisite_id parent FROM photo.task_edges e WHERE e.tenant_id=p_tenant AND e.dependent_id=ANY(ids)
 UNION SELECT i.task_id,a.created_by_task_id FROM photo.v2_task_inputs i JOIN photo.artifacts a
 ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=ANY(ids) AND a.created_by_task_id IS NOT NULL AND a.created_by_task_id<>i.task_id
 UNION SELECT t.id,a.created_by_task_id FROM photo.tasks t CROSS JOIN LATERAL jsonb_array_elements(t.inputs) input
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id::text=input->>'artifact_id' AND a.sha256=input->>'sha256'
 WHERE t.tenant_id=p_tenant AND t.id=ANY(ids) AND a.created_by_task_id IS NOT NULL AND a.created_by_task_id<>t.id
 ), bounded AS (SELECT child,parent FROM dependency ORDER BY child,parent LIMIT p_max_edges+1)
 SELECT count(*),coalesce(jsonb_agg(jsonb_build_object('child',child,'parent',parent) ORDER BY child,parent),'[]'::jsonb) INTO edge_count,parents FROM bounded;
 IF edge_count>p_max_edges OR EXISTS(SELECT 1 FROM jsonb_array_elements(parents) e WHERE NOT (e->>'parent')::uuid=ANY(ids)) THEN RETURN NULL;END IF;
 -- Read-only preview of the exact canonical negative result is already
 -- permitted by the consumer barrier and review completion. All other outside
 -- consumers, including execution edges without artifact reads, make this
 -- project-only cancellation inventory incomplete.
 IF EXISTS(
 WITH outside_dependency AS (
  SELECT e.dependent_id child,e.prerequisite_id parent FROM photo.task_edges e
  WHERE e.tenant_id=p_tenant AND e.prerequisite_id=ANY(ids) AND NOT e.dependent_id=ANY(ids)
  UNION
  SELECT consumer.id,a.created_by_task_id FROM photo.artifacts a
  JOIN photo.tasks consumer ON consumer.tenant_id=a.tenant_id
  WHERE a.tenant_id=p_tenant AND a.created_by_task_id=ANY(ids) AND NOT consumer.id=ANY(ids)
  AND (consumer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
   OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=consumer.tenant_id
    AND i.task_id=consumer.id AND i.artifact_id=a.id AND i.sha256=a.sha256))
 )
 SELECT 1 FROM outside_dependency d
 JOIN photo.tasks producer ON producer.tenant_id=p_tenant AND producer.id=d.parent
 JOIN photo.tasks consumer ON consumer.tenant_id=p_tenant AND consumer.id=d.child
 WHERE NOT EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=consumer.run_id
  AND producer.operation='optical.negative' AND producer.operation_version='1'
  AND producer.canonical_result_id IS NOT NULL AND r.kind='preview'
  AND r.v26_derivative_task_id=producer.id
  AND r.config_snapshot->>'source_result_id'=producer.canonical_result_id::text)
 ) THEN RETURN NULL;END IF;
 SELECT jsonb_agg(jsonb_build_object('task_id',t.id,'run_id',t.run_id,'parents',coalesce((
 SELECT jsonb_agg(e->'parent' ORDER BY e->>'parent') FROM jsonb_array_elements(parents) e WHERE e->>'child'=t.id::text),'[]'::jsonb)) ORDER BY t.id)
 INTO result FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=ANY(ids);
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_negative_dependency_inventory(uuid,uuid,uuid,uuid,uuid,integer,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_negative_dependency_inventory(uuid,uuid,uuid,uuid,uuid,integer,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_negative_dependency_inventory(uuid,uuid,uuid,uuid,uuid,integer,integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
