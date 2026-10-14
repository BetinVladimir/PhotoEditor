-- +goose Up
-- Dynamic activation closes the current materialization barrier, not the
-- physical verification of the operation it is about to dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v30_delivery_continuation(p_expansion uuid,p_token uuid,p_slot integer,p_stage integer,
 p_predecessor uuid,p_task jsonb,p_params text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 previous photo.tasks%ROWTYPE;b photo.graph_expansions%ROWTYPE;q photo.quota_reservations%ROWTYPE;
BEGIN
 IF photo.v30_delivery_continuation_transport_ready(p_expansion,p_token,p_slot,p_stage,p_predecessor,p_task,p_params) IS DISTINCT FROM true THEN RETURN false;END IF;
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(expected->>'child_run_id')::uuid FOR UPDATE;
 SELECT * INTO b FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=r.id AND kind='v30.delivery.export/1' FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=(p_task->>'id')::uuid FOR UPDATE;
 SELECT * INTO previous FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=p_predecessor FOR UPDATE;
 IF r.id IS NULL OR r.status<>'running' OR r.cancel_requested OR r.graph_sealed
 OR b.id IS NULL OR b.state<>'open' OR b.cursor IS DISTINCT FROM (p_stage-1)::text
 OR t.id IS NULL OR t.state<>'blocked' OR t.cancel_requested
 OR previous.id IS NULL OR previous.state<>'succeeded' OR previous.canonical_result_id IS NULL
 OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v30-stage-verified'
 AND event_id=photo.v2_adoption_event_id(previous.id,previous.canonical_result_id)) THEN RETURN false;END IF;
 PERFORM version FROM photo.operation_registry WHERE operation=t.operation AND version=t.operation_version
 AND resource_class='cpu' AND v2_enabled AND backends ? 'cpu' FOR SHARE;
 IF NOT FOUND OR t.operation NOT IN('derivative.sharpen','export.encode')
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=e.tenant_id AND holder_type='run' AND holder_id=r.id
 AND state='reserved' AND expires_at>clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR q.bytes<1 OR NOT EXISTS(SELECT 1 FROM photo.tenant_usage WHERE tenant_id=e.tenant_id AND reserved_bytes>=q.bytes) THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=r.id AND id<>b.id AND state<>'closed') THEN RETURN false;END IF;
 IF t.operation='export.encode' THEN
  UPDATE photo.exports SET task_id=t.id WHERE tenant_id=e.tenant_id AND id=(r.config_snapshot->>'export_id')::uuid
  AND status='queued' AND (task_id IS NULL OR task_id=t.id);
  IF NOT FOUND THEN RETURN false;END IF;
 END IF;
 UPDATE photo.graph_expansions SET cursor=p_stage::text,state='closed' WHERE id=b.id;
 UPDATE photo.runs SET graph_sealed=true,revision=revision+1 WHERE tenant_id=e.tenant_id AND id=r.id;
 IF NOT photo.activate_bound_task(t.id,t.input_digest,t.params_artifact_id) THEN
  RAISE EXCEPTION 'DELIVERY_CONTINUATION_ACTIVATION_REFUSED' USING ERRCODE='23514';END IF;
 -- The predecessor can be acknowledged only after the successor's validated
 -- inputs and runnable graph are committed in this same transaction.
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at)
 VALUES(e.tenant_id,'v2-adopt',photo.v2_adoption_event_id(previous.id,previous.canonical_result_id),clock_timestamp()) ON CONFLICT DO NOTHING;
 IF e.expansion_lease_until<=clock_timestamp() THEN RAISE EXCEPTION 'DELIVERY_CONTINUATION_ACTIVATION_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.activate_v30_delivery_continuation(uuid,uuid,integer,integer,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_delivery_continuation(uuid,uuid,integer,integer,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.activate_v30_delivery_continuation(uuid,uuid,integer,integer,uuid,jsonb,text) TO photo_scheduler;

CREATE FUNCTION photo.reopen_v30_delivery_continuation(p_expansion uuid,p_token uuid,p_slot integer,p_producer jsonb,p_image text,p_report text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;b photo.graph_expansions%ROWTYPE;
BEGIN
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF expected IS NULL OR ((expected->>'config_jcs')::jsonb)->'size' IS NULL
 OR ((expected->>'config_jcs')::jsonb)->'sharpen' IS NULL THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(expected->>'child_run_id')::uuid FOR UPDATE;
 SELECT * INTO b FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=r.id AND kind='v30.delivery.export/1' FOR UPDATE;
 IF r.id IS NULL OR r.status<>'running' OR r.cancel_requested OR b.id IS NULL
 OR b.cursor IS DISTINCT FROM '1' OR b.expected_count<>2 OR b.state NOT IN('open','closed') THEN RETURN false;END IF;
 IF NOT photo.record_v30_delivery_stage_verification(p_expansion,p_token,p_slot,'derivative.sharpen',p_producer,p_image,p_report) THEN RETURN false;END IF;
 IF b.state='open' THEN RETURN NOT r.graph_sealed;END IF;
 UPDATE photo.graph_expansions SET state='open' WHERE id=b.id;
 UPDATE photo.runs SET graph_sealed=false,revision=revision+1 WHERE tenant_id=e.tenant_id AND id=r.id;
 IF e.expansion_lease_until<=clock_timestamp() THEN RAISE EXCEPTION 'DELIVERY_CONTINUATION_REOPEN_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.reopen_v30_delivery_continuation(uuid,uuid,integer,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reopen_v30_delivery_continuation(uuid,uuid,integer,jsonb,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reopen_v30_delivery_continuation(uuid,uuid,integer,jsonb,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
