-- +goose Up
-- Private transaction component only; caller must commit parent/receipt/events atomically.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_semantic_shown_apply_child(p_tenant uuid,p_project uuid,p_parent uuid,p_child uuid,p_actor uuid,p_tasks jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE q photo.quota_reservations%ROWTYPE; expected numeric; changed integer;row jsonb;
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
 IF photo.semantic_shown_apply_sealed_inputs_ready(p_tenant,p_project,p_actor,p_child,p_tasks) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.v2_review_batches b JOIN photo.v2_workflows w
 ON w.tenant_id=b.tenant_id AND w.id=b.workflow_id AND w.run_id=p_parent
 WHERE b.tenant_id=p_tenant AND b.project_id=p_project AND b.child_run_id=p_child
 AND b.actor_id=p_actor AND b.action='apply') THEN RETURN false;END IF;
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
 IF photo.v27_downstream_profile_ready(p_tenant,p_project,'semantic.adopt_shown_exposure','1','cpu',row->'node'->'execution_profile') IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 SELECT sum((x.value->'node'->'execution_profile'->'resource_estimate'->>'output_bytes')::numeric)
 INTO expected FROM jsonb_array_elements(p_tasks) AS x(value);
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_child FOR UPDATE;
 IF expected IS NULL OR expected NOT BETWEEN 1 AND 9007199254740991 OR q.id IS NULL
 OR q.state IS DISTINCT FROM 'reserved' OR q.bytes IS DISTINCT FROM expected::bigint OR q.files IS DISTINCT FROM 0 OR q.principal_type IS DISTINCT FROM 'user'
 OR q.principal_id IS DISTINCT FROM p_actor OR q.share_id IS NOT NULL OR q.expires_at IS NULL OR q.expires_at<=clock_timestamp() THEN RETURN false;END IF;
 UPDATE photo.tasks SET state='ready' WHERE tenant_id=p_tenant AND run_id=p_child AND state='blocked' AND attempt=0 AND NOT cancel_requested;
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>jsonb_array_length(p_tasks) THEN RAISE EXCEPTION 'SEMANTIC_APPLY_ACTIVATION_PARTIAL' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET graph_sealed=true,status='queued' WHERE tenant_id=p_tenant AND id=p_child AND status='draft' AND NOT graph_sealed AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_APPLY_ACTIVATION_STALE' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.activate_semantic_shown_apply_child(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_semantic_shown_apply_child(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
