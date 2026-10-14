-- +goose Up
-- Atomically seal successor graph, dispatch encoder and adopt verified watermark.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v30_watermark_encode(p_expansion uuid,p_token uuid,p_slot integer,p_stage integer,
 p_predecessor uuid,p_task jsonb,p_params text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 previous photo.tasks%ROWTYPE;b photo.graph_expansions%ROWTYPE;q photo.quota_reservations%ROWTYPE;
BEGIN
 IF photo.v30_watermark_encode_transport_ready(p_expansion,p_token,p_slot,p_stage,p_predecessor,p_task,p_params) IS DISTINCT FROM true THEN RETURN false;END IF;
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(expected->>'child_run_id')::uuid FOR UPDATE;
 SELECT * INTO b FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=r.id AND kind='v30.delivery.watermark_encode/1' FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=(p_task->>'id')::uuid FOR UPDATE;
 SELECT * INTO previous FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=p_predecessor FOR UPDATE;
 IF r.id IS NULL OR r.status<>'running' OR r.cancel_requested OR r.graph_sealed
 OR b.id IS NULL OR b.state<>'open' OR b.cursor IS DISTINCT FROM '0' OR b.expected_count<>1 OR b.parent_task_id IS DISTINCT FROM p_predecessor
 OR t.id IS NULL OR t.state<>'blocked' OR t.cancel_requested
 OR previous.id IS NULL OR previous.state<>'succeeded' OR previous.cancel_requested OR previous.operation<>'derivative.watermark' OR previous.canonical_result_id IS NULL
 OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v30-watermark-stage-verified'
 AND event_id=photo.v2_adoption_event_id(previous.id,previous.canonical_result_id)) THEN RETURN false;END IF;
 PERFORM version FROM photo.operation_registry WHERE operation=t.operation AND version=t.operation_version
 AND resource_class='cpu' AND v2_enabled AND backends ? 'cpu' FOR SHARE;
 IF NOT FOUND OR t.operation IS DISTINCT FROM 'export.encode'
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
 UPDATE photo.graph_expansions SET cursor='1',state='closed' WHERE id=b.id;
 UPDATE photo.runs SET graph_sealed=true,revision=revision+1 WHERE tenant_id=e.tenant_id AND id=r.id;
 IF photo.activate_bound_task(t.id,t.input_digest,t.params_artifact_id) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'DELIVERY_CONTINUATION_ACTIVATION_REFUSED' USING ERRCODE='23514';END IF;
 -- The predecessor can be acknowledged only after the successor's validated
 -- inputs and runnable graph are committed in this same transaction.
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at)
 VALUES(e.tenant_id,'v2-adopt',photo.v2_adoption_event_id(previous.id,previous.canonical_result_id),clock_timestamp()) ON CONFLICT DO NOTHING;
 IF e.expansion_lease_until<=clock_timestamp() THEN RAISE EXCEPTION 'DELIVERY_CONTINUATION_ACTIVATION_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.activate_v30_watermark_encode(uuid,uuid,integer,integer,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_watermark_encode(uuid,uuid,integer,integer,uuid,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.activate_v30_watermark_encode(uuid,uuid,integer,integer,uuid,jsonb,text) TO photo_scheduler;

CREATE FUNCTION photo.v30_watermark_encode_activated(p_expansion uuid,p_token uuid,p_slot integer,p_stage integer,
 p_predecessor uuid,p_task jsonb,p_params text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;cfg jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;
 b photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;previous photo.tasks%ROWTYPE;d photo.backend_documents%ROWTYPE;
 operation text;previous_operation text;required_stage integer;owner uuid;
BEGIN
 IF p_stage IS NULL OR p_stage NOT BETWEEN 1 AND 3 OR p_predecessor IS NULL OR p_task IS NULL
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF request IS NULL THEN RETURN false;END IF;
 cfg:=(request->>'config_jcs')::jsonb;operation:=p_task->>'operation';
 IF operation IS DISTINCT FROM 'export.encode' OR jsonb_typeof(cfg->'watermark') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 required_stage:=1+CASE WHEN cfg->'size' IS NOT NULL THEN 1 ELSE 0 END+CASE WHEN cfg->'sharpen' IS NOT NULL THEN 1 ELSE 0 END;
 previous_operation:='derivative.watermark';
 IF photo.lock_v30_watermark_stage_source(p_expansion,p_token,p_slot) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF p_stage<>required_stage THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(request->>'child_run_id')::uuid FOR SHARE;
 IF NOT FOUND OR r.kind<>'v2' OR r.config_snapshot->'v30_delivery_export' IS DISTINCT FROM request THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=r.id AND kind='v30.delivery.watermark_encode/1' FOR SHARE;
 IF NOT FOUND OR b.cursor IS DISTINCT FROM '1' OR b.state<>'closed' OR b.expected_count<>1 OR b.parent_task_id IS DISTINCT FROM p_predecessor THEN RETURN false;END IF;
 owner:=photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),operation||':0');
 IF p_predecessor<>photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),previous_operation||':0') THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=owner FOR SHARE;
 IF NOT FOUND OR t.state NOT IN('ready','leased','running','succeeded','failed','cancelled','skipped')
 OR t.id::text IS DISTINCT FROM p_task->>'id' OR t.node_key IS DISTINCT FROM operation||':0'
 OR t.node_key IS DISTINCT FROM p_task->>'node_key' OR t.operation IS DISTINCT FROM operation
 OR t.operation_version IS DISTINCT FROM p_task->>'operation_version' OR t.resource_class IS DISTINCT FROM p_task->>'resource_class'
 OR t.params_artifact_id::text IS DISTINCT FROM p_task->>'params_artifact_id'
 OR t.input_digest IS DISTINCT FROM p_task->>'input_digest'
 OR photo.v30_delivery_profile_matches(t.execution_profile,p_task->'execution_profile') IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND owner_id=t.id
 AND purpose='task_params' AND artifact_id=t.params_artifact_id AND state='ready';
 IF NOT FOUND OR d.sha256 IS DISTINCT FROM p_task->>'params_sha'
 OR d.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_params,'UTF8')),'hex') OR d.bytes<>octet_length(p_params) THEN RETURN false;END IF;
 SELECT * INTO previous FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=p_predecessor;
 IF NOT FOUND OR previous.canonical_result_id IS NULL
 OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(previous.id,previous.canonical_result_id))
 OR NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=e.tenant_id AND run_id=r.id
 AND prerequisite_id=p_predecessor AND dependent_id=t.id AND condition='success') THEN RETURN false;END IF;
 IF operation='export.encode' AND NOT EXISTS(SELECT 1 FROM photo.exports WHERE tenant_id=e.tenant_id
 AND id=(r.config_snapshot->>'export_id')::uuid AND task_id=t.id) THEN RETURN false;END IF;
 RETURN e.expansion_lease_until>clock_timestamp();
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_watermark_encode_activated(uuid,uuid,integer,integer,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_encode_activated(uuid,uuid,integer,integer,uuid,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_watermark_encode_activated(uuid,uuid,integer,integer,uuid,jsonb,text) TO photo_scheduler;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
