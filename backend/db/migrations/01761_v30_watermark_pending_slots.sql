-- +goose Up
-- Discover unadopted verified-result candidates under current pin authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_pending_child_slots(p_expansion uuid,p_token uuid,p_limit integer) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE payload jsonb;e photo.graph_expansions%ROWTYPE;slots jsonb;
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 100 THEN RETURN NULL;END IF;
 payload:=photo.v30_delivery_expansion_plan(p_expansion,p_token);
 IF payload IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion AND tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT coalesce(jsonb_agg(q.slot ORDER BY q.slot),'[]'::jsonb) INTO slots FROM (
 SELECT DISTINCT (r.config_snapshot->'v30_delivery_export'->>'slot_index')::integer AS slot
 FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 WHERE r.tenant_id=e.tenant_id AND r.kind='v2' AND r.status='running' AND NOT r.cancel_requested
 AND r.config_snapshot->'v30_delivery_export'->>'parent_run_id'=e.run_id::text
 AND jsonb_typeof((r.config_snapshot->'v30_delivery_export'->>'config_jcs')::jsonb->'watermark')='object'
 AND r.config_snapshot->'v30_delivery_export' IS NOT DISTINCT FROM photo.v30_delivery_child_request(e.id,p_token,(r.config_snapshot->'v30_delivery_export'->>'slot_index')::integer)
 AND EXISTS(SELECT 1 FROM photo.read_v30_watermark_pin(e.tenant_id,e.run_id,e.id,p_token,
 (r.config_snapshot->'v30_delivery_export'->'preset'->>'preset_id')::uuid,r.config_snapshot->'v30_delivery_export'->'preset'->>'preset_sha'))
 AND t.operation IN('derivative.resize','derivative.sharpen','derivative.watermark') AND t.state='succeeded' AND NOT t.cancel_requested
 AND t.canonical_result_id IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 AND (r.config_snapshot->'v30_delivery_export'->>'slot_index')::integer<e.cursor::integer
 ORDER BY slot LIMIT p_limit) q;
 IF e.expansion_lease_until<=clock_timestamp() THEN RETURN NULL;END IF;
 RETURN slots;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_watermark_pending_child_slots(uuid,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_pending_child_slots(uuid,uuid,integer) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_watermark_pending_child_slots(uuid,uuid,integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
