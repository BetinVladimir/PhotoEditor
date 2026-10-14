-- Preserve exact SeriesStats refs alongside series evidence in the pending
-- receipt. The older series validator still checks all existing fields.
-- This does not remove the series Fit/Evaluate freeze gate.
-- +goose Up
ALTER FUNCTION photo.save_v23_training_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 RENAME TO save_v23_training_recovery_pre_series_stats_core;
REVOKE ALL ON FUNCTION photo.save_v23_training_recovery_pre_series_stats_core(
 uuid,uuid,uuid,uuid,text,text,jsonb) FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.save_v23_training_recovery(p_tenant uuid,p_actor uuid,
 p_auth_session uuid,p_run uuid,p_key text,p_request_sha text,p_body jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE base jsonb; stats_items jsonb; series_items jsonb; item jsonb;
 ref jsonb; expected_stats jsonb; source_row record;
 seen_events uuid[]:=ARRAY[]::uuid[];
 seen_refs uuid[]; event_id uuid; artifact_id uuid; project_id uuid;
 stat_row record; idem photo.idempotency_records%ROWTYPE; saved boolean;
BEGIN
 IF p_body IS NULL OR jsonb_typeof(p_body) IS DISTINCT FROM 'object' OR
  octet_length(p_body::text)>33554432 THEN
  RAISE EXCEPTION 'V23_SERIES_STATS_RECOVERY_INVALID' USING ERRCODE='22023';
 END IF;
 IF NOT p_body ? 'series_jcs' THEN
  IF p_body ? 'series_stats_jcs' THEN
   RAISE EXCEPTION 'V23_SERIES_STATS_RECOVERY_INVALID' USING ERRCODE='22023';
  END IF;
  RETURN photo.save_v23_training_recovery_pre_series_stats_core(
   p_tenant,p_actor,p_auth_session,p_run,p_key,p_request_sha,p_body);
 END IF;
 IF jsonb_typeof(p_body->'series_stats_jcs') IS DISTINCT FROM 'string' OR
  jsonb_typeof(p_body->'series_jcs') IS DISTINCT FROM 'string' OR
  jsonb_typeof(p_body->'project_id') IS DISTINCT FROM 'string' OR
  octet_length(p_body->>'series_stats_jcs') NOT BETWEEN 2 AND 16777216 THEN
  RAISE EXCEPTION 'V23_SERIES_STATS_RECOVERY_INVALID' USING ERRCODE='22023';
 END IF;
 project_id:=(p_body->>'project_id')::uuid;
 stats_items:=(p_body->>'series_stats_jcs')::jsonb;
 series_items:=(p_body->>'series_jcs')::jsonb;
 IF jsonb_typeof(stats_items) IS DISTINCT FROM 'array' OR
  jsonb_typeof(series_items) IS DISTINCT FROM 'array' OR
  jsonb_array_length(stats_items) NOT BETWEEN 1 AND 5000 OR
  jsonb_array_length(stats_items)<>jsonb_array_length(series_items) THEN
  RAISE EXCEPTION 'V23_SERIES_STATS_RECOVERY_INVALID' USING ERRCODE='22023';
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(stats_items) LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR
   (item-'event_id'-'stats')<>'{}'::jsonb OR
   jsonb_typeof(item->'event_id') IS DISTINCT FROM 'string' OR
   jsonb_typeof(item->'stats') IS DISTINCT FROM 'array' OR
   jsonb_array_length(item->'stats') NOT BETWEEN 2 AND 2000 THEN
   RAISE EXCEPTION 'V23_SERIES_STATS_RECOVERY_INVALID' USING ERRCODE='22023';
  END IF;
  event_id:=(item->>'event_id')::uuid;
  IF event_id=ANY(seen_events) OR
   (SELECT count(*) FROM jsonb_array_elements(series_items) x WHERE
    x.value->>'event_id'=event_id::text)<>1 THEN
   RAISE EXCEPTION 'V23_SERIES_STATS_RECOVERY_CONFLICT' USING ERRCODE='23514';
  END IF;
  seen_events:=array_append(seen_events,event_id);
  SELECT * INTO source_row FROM photo.v23_series_event_source(
   p_tenant,project_id,p_actor,p_auth_session,event_id);
  IF NOT FOUND THEN
   RAISE EXCEPTION 'V23_SERIES_STATS_RECOVERY_SOURCE' USING ERRCODE='23514';
  END IF;
  SELECT bn.params->'stats' INTO expected_stats FROM photo.apply_nodes bn
   WHERE bn.tenant_id=p_tenant AND bn.run_id=source_row.apply_run_id AND
    bn.id=source_row.producer_task_id AND bn.phase='series_balance' AND
    bn.operation='series.balance';
  IF expected_stats IS DISTINCT FROM item->'stats' THEN
   RAISE EXCEPTION 'V23_SERIES_STATS_RECOVERY_CONFLICT' USING ERRCODE='23514';
  END IF;
  seen_refs:=ARRAY[]::uuid[];
  FOR ref IN SELECT value FROM jsonb_array_elements(item->'stats') LOOP
   IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR
    (ref-'artifact_id'-'sha256')<>'{}'::jsonb OR
    jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string' OR
    jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string' OR
    ref->>'sha256' !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'V23_SERIES_STATS_RECOVERY_INVALID' USING ERRCODE='22023';
   END IF;
   artifact_id:=(ref->>'artifact_id')::uuid;
   IF artifact_id=ANY(seen_refs) THEN
    RAISE EXCEPTION 'V23_SERIES_STATS_RECOVERY_DUPLICATE' USING ERRCODE='23514';
   END IF;
   seen_refs:=array_append(seen_refs,artifact_id);
   SELECT * INTO stat_row FROM photo.v23_series_stat_artifact(
    p_tenant,project_id,p_actor,p_auth_session,event_id,
    artifact_id,ref->>'sha256');
   IF NOT FOUND THEN
    RAISE EXCEPTION 'V23_SERIES_STATS_RECOVERY_SOURCE' USING ERRCODE='23514';
   END IF;
  END LOOP;
 END LOOP;
 base:=p_body-'series_stats_jcs';
 SELECT * INTO idem FROM photo.idempotency_records x WHERE
  x.tenant_id=p_tenant AND x.principal_type='user' AND
  x.principal_id=p_actor AND x.route='V2.Admit' AND x.key=p_key AND
  x.request_hash=p_request_sha FOR UPDATE;
 IF idem.id IS NOT NULL AND idem.response_status IS NULL AND
  idem.response_body=p_body THEN
  UPDATE photo.idempotency_records SET response_body=base WHERE id=idem.id;
 END IF;
 saved:=photo.save_v23_training_recovery_pre_series_stats_core(
  p_tenant,p_actor,p_auth_session,p_run,p_key,p_request_sha,base);
 IF NOT saved THEN RETURN false; END IF;
 UPDATE photo.idempotency_records SET response_body=p_body WHERE
  tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor AND
  route='V2.Admit' AND key=p_key AND request_hash=p_request_sha AND
  response_status IS NULL AND response_body=base;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'V23_SERIES_STATS_RECOVERY_CONFLICT' USING ERRCODE='23514';
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR
 invalid_parameter_value THEN
 RAISE EXCEPTION 'V23_SERIES_STATS_RECOVERY_INVALID' USING ERRCODE='22023';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.save_v23_training_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v23_training_recovery(
 uuid,uuid,uuid,uuid,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v23_training_recovery(
 uuid,uuid,uuid,uuid,text,text,jsonb) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
