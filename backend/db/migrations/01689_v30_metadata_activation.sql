-- +goose Up
-- Activation requires preexisting authenticated barrier/proof and sealed inputs.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v30_metadata_task(p_tenant uuid,p_expansion uuid,p_token uuid,p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 predecessor photo.tasks%ROWTYPE;q photo.quota_reservations%ROWTYPE;reserved bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT reserved_bytes INTO reserved FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF photo.v30_metadata_transport_ready(p_tenant,p_expansion,p_token,p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=e.run_id FOR UPDATE;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND id=(p_task->>'id')::uuid FOR UPDATE;
 SELECT * INTO predecessor FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND id=e.parent_task_id FOR UPDATE;
 IF r.id IS NULL OR r.status<>'running' OR r.cancel_requested OR r.graph_sealed
 OR r.revision>=9007199254740991 OR e.state<>'open' OR e.kind<>'v30.delivery.metadata/1'
 OR e.expected_count<>1 OR e.expansion_token IS DISTINCT FROM p_token
 OR e.expansion_lease_until IS NULL OR e.expansion_lease_until<=clock_timestamp()
 OR t.id IS NULL OR t.state<>'blocked' OR t.cancel_requested
 OR predecessor.id IS NULL OR predecessor.state<>'succeeded' OR predecessor.cancel_requested
 OR predecessor.operation<>'export.encode' OR predecessor.canonical_result_id IS NULL
 OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-encode-verified'
 AND event_id=photo.v2_adoption_event_id(predecessor.id,predecessor.canonical_result_id)) THEN RETURN false;END IF;
 PERFORM version FROM photo.operation_registry WHERE operation='metadata.write' AND version='1'
 AND resource_class='cpu' AND v2_enabled AND backends ? 'cpu' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=r.id
 AND state='reserved' AND expires_at>clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR q.bytes<1 OR reserved<q.bytes THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=r.id AND id<>e.id AND state<>'closed') THEN RETURN false;END IF;
 IF photo.v30_metadata_transport_ready(p_tenant,p_expansion,p_token,p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.graph_expansions SET cursor='1',state='closed' WHERE id=e.id;
 UPDATE photo.runs SET graph_sealed=true,revision=revision+1 WHERE tenant_id=p_tenant AND id=r.id;
 IF NOT photo.activate_bound_task(t.id,t.input_digest,t.params_artifact_id) THEN
 RAISE EXCEPTION 'DELIVERY_METADATA_ACTIVATION_REFUSED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at)
 VALUES(p_tenant,'v2-adopt',photo.v2_adoption_event_id(predecessor.id,predecessor.canonical_result_id),clock_timestamp()) ON CONFLICT DO NOTHING;
 IF e.expansion_lease_until<=clock_timestamp() THEN RAISE EXCEPTION 'DELIVERY_METADATA_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.activate_v30_metadata_task(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_metadata_task(uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.activate_v30_metadata_task(uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
