-- +goose Up
-- Recover an admitted child without registering inputs on a running task.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_child_receipt(p_expansion uuid,p_token uuid,p_slot integer,p_request text,p_export uuid,p_task jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;e photo.graph_expansions%ROWTYPE;child photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 w photo.v2_workflows%ROWTYPE;d photo.delivery_packages%ROWTYPE;artifact_sha text;
BEGIN
 IF p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 2097152 OR p_export IS NULL
 OR p_task IS NULL OR jsonb_typeof(p_task) IS DISTINCT FROM 'object' OR pg_column_size(p_task)>65536 THEN RETURN NULL;END IF;
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF expected IS NULL OR p_request::jsonb IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 IF p_slot>=e.cursor::integer THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=e.tenant_id AND run_id=e.run_id;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=e.tenant_id AND workflow_id=w.id;
 SELECT * INTO child FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(expected->>'child_run_id')::uuid FOR SHARE;
 IF child.id IS NULL OR child.kind<>'v2' OR child.status NOT IN('queued','running','waiting_review','completed','failed','cancelling','cancelled')
 OR child.config_snapshot IS DISTINCT FROM jsonb_build_object('project_id',w.project_id,'v30_delivery_export',expected,'export_id',p_export)
 OR NOT EXISTS(SELECT 1 FROM photo.delivery_items i WHERE i.tenant_id=e.tenant_id AND i.package_id=d.id
 AND i.ordinal=(expected->>'ordinal')::integer AND i.preset_id=(expected->'preset'->>'preset_id')::uuid AND i.export_id=p_export) THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=child.id AND id=(p_task->>'id')::uuid FOR SHARE;
 SELECT sha256 INTO artifact_sha FROM photo.artifacts WHERE tenant_id=e.tenant_id AND id=t.params_artifact_id;
 IF t.id IS NULL OR p_task IS DISTINCT FROM jsonb_build_object('id',t.id,'node_key',t.node_key,'operation',t.operation,
 'operation_version',t.operation_version,'resource_class',t.resource_class,'params_artifact_id',t.params_artifact_id,
 'params_sha',artifact_sha,'input_digest',t.input_digest,'execution_profile',t.execution_profile-'v2_input_manifest') THEN RETURN NULL;END IF;
 -- Slot binding and cursor were committed atomically with initial graph seal.
 -- Current graph_sealed may be false during a later derivative expansion.
 RETURN jsonb_build_object('run_id',child.id,'task_id',t.id,'export_id',p_export,'cursor',e.cursor::integer,'status',child.status,'graph_sealed',child.graph_sealed);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_child_receipt(uuid,uuid,integer,text,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_child_receipt(uuid,uuid,integer,text,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_child_receipt(uuid,uuid,integer,text,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
