-- +goose Up
-- Generic sealing cannot dispatch a delivery child without its cursor receipt.
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) RENAME TO seal_v2_graph_pre_v30_child;
REVOKE ALL ON FUNCTION photo.seal_v2_graph_pre_v30_child(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND config_snapshot ? 'v30_delivery_export') THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_pre_v30_child(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;

CREATE FUNCTION photo.activate_v30_delivery_child(p_expansion uuid,p_token uuid,p_slot integer,p_request text,p_export uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;e photo.graph_expansions%ROWTYPE;parent photo.runs%ROWTYPE;child photo.runs%ROWTYPE;
 w photo.v2_workflows%ROWTYPE;config jsonb;
BEGIN
 IF p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 2097152 OR p_export IS NULL THEN RETURN false;END IF;
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF expected IS NULL OR p_request::jsonb IS DISTINCT FROM expected THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=e.tenant_id AND id=e.run_id;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=e.tenant_id AND run_id=e.run_id;
 SELECT * INTO child FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(expected->>'child_run_id')::uuid FOR UPDATE;
 config:=jsonb_build_object('project_id',w.project_id,'v30_delivery_export',expected,'export_id',p_export);
 IF child.id IS NULL OR child.kind<>'v2' OR child.created_by IS DISTINCT FROM parent.created_by
 OR child.input_snapshot_id IS DISTINCT FROM parent.input_snapshot_id OR child.cancel_requested
 OR child.config_snapshot IS DISTINCT FROM config THEN RETURN false;END IF;
 -- For an acknowledgement retry the cursor writer verifies the saved binding.
 IF child.graph_sealed THEN
  RETURN photo.commit_v30_delivery_child(p_expansion,p_token,p_slot,p_request,p_export);
 END IF;
 IF child.status<>'draft' OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=child.id)<>1
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=e.tenant_id AND run_id=child.id)
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=child.id)
 OR NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=child.id
 AND state='blocked' AND attempt=0 AND NOT cancel_requested AND resource_class='cpu'
 AND operation IN('derivative.resize','derivative.sharpen','export.encode') AND operation_version='1') THEN RETURN false;END IF;
 -- Shared registry/qualification/resource/closure/quota and outbox checks apply.
 IF NOT photo.seal_v2_graph_pre_v30_child(e.tenant_id,child.id) THEN RETURN false;END IF;
 IF NOT photo.commit_v30_delivery_child(p_expansion,p_token,p_slot,p_request,p_export) THEN
  RAISE EXCEPTION 'DELIVERY_CHILD_ACTIVATION_REFUSED' USING ERRCODE='23514';
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.activate_v30_delivery_child(uuid,uuid,integer,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_delivery_child(uuid,uuid,integer,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.activate_v30_delivery_child(uuid,uuid,integer,text,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
