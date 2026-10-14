-- +goose Up
-- Private transaction component only; caller must commit parent/receipt/events atomically.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_semantic_apply_child(p_tenant uuid,p_project uuid,p_parent uuid,p_child uuid,p_actor uuid,p_params jsonb,p_nodes jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE q photo.quota_reservations%ROWTYPE; expected numeric; changed integer;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_parent AND kind='v2'
 AND status='waiting_review' AND NOT cancel_requested FOR UPDATE;
 IF NOT FOUND OR p_parent=p_child THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child AND kind='apply'
 AND status='draft' AND NOT graph_sealed AND NOT cancel_requested AND created_by=p_actor
 AND config_snapshot->>'project_id'=p_project::text FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child ORDER BY id FOR UPDATE;
 IF NOT photo.semantic_approved_exposure_nodes_ready(p_tenant,p_project,p_child,p_params,p_nodes)
 OR NOT photo.semantic_apply_sealed_inventory_ready(p_tenant,p_project,p_child,p_nodes,p_transport,p_inventory)
 OR NOT EXISTS(SELECT 1 FROM photo.v2_review_batches b JOIN photo.v2_workflows w
 ON w.tenant_id=b.tenant_id AND w.id=b.workflow_id AND w.run_id=p_parent
 WHERE b.tenant_id=p_tenant AND b.project_id=p_project AND b.child_run_id=p_child
 AND b.actor_id=p_actor AND b.action='apply') THEN RETURN false;END IF;
 SELECT sum((n->'execution_profile'->'resource_estimate'->>'output_bytes')::numeric)
 INTO expected FROM jsonb_array_elements(p_nodes) n;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_child FOR UPDATE;
 IF expected IS NULL OR expected NOT BETWEEN 1 AND 9007199254740991 OR q.id IS NULL
 OR q.state<>'reserved' OR q.bytes<>expected OR q.files<>0 OR q.principal_type<>'user'
 OR q.principal_id IS DISTINCT FROM p_actor OR q.share_id IS NOT NULL OR q.expires_at<=clock_timestamp() THEN RETURN false;END IF;
 UPDATE photo.tasks SET state='ready' WHERE tenant_id=p_tenant AND run_id=p_child AND state='blocked' AND attempt=0 AND NOT cancel_requested;
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>jsonb_array_length(p_nodes) THEN RAISE EXCEPTION 'SEMANTIC_APPLY_ACTIVATION_PARTIAL' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET graph_sealed=true,status='queued' WHERE tenant_id=p_tenant AND id=p_child AND status='draft' AND NOT graph_sealed AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_APPLY_ACTIVATION_STALE' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.activate_semantic_apply_child(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_semantic_apply_child(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
