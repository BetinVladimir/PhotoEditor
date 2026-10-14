-- +goose Up
-- Bounded observation candidates and pre-publication recovery of frozen Params.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_pending_child_slots(p_expansion uuid,p_token uuid,p_limit integer) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE payload jsonb;e photo.graph_expansions%ROWTYPE;slots jsonb;
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 100 THEN RETURN NULL;END IF;
 payload:=photo.v30_delivery_expansion_plan(p_expansion,p_token);
 IF payload IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT coalesce(jsonb_agg(q.slot ORDER BY q.slot),'[]'::jsonb) INTO slots FROM (
 SELECT DISTINCT (r.config_snapshot->'v30_delivery_export'->>'slot_index')::integer AS slot
 FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 WHERE r.tenant_id=e.tenant_id AND r.kind='v2' AND r.status='running' AND NOT r.cancel_requested
 AND r.config_snapshot->'v30_delivery_export'->>'parent_run_id'=e.run_id::text
 AND t.operation IN('derivative.resize','derivative.sharpen') AND t.state='succeeded' AND NOT t.cancel_requested
 AND t.canonical_result_id IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 AND (r.config_snapshot->'v30_delivery_export'->>'slot_index')::integer<e.cursor::integer
 ORDER BY slot LIMIT p_limit) q;
 IF e.expansion_lease_until<=clock_timestamp() THEN RETURN NULL;END IF;
 RETURN slots;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_pending_child_slots(uuid,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_pending_child_slots(uuid,uuid,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_pending_child_slots(uuid,uuid,integer) TO photo_scheduler;

CREATE FUNCTION photo.v30_delivery_saved_continuation_params(p_expansion uuid,p_token uuid,p_slot integer,p_operation text,p_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;e photo.graph_expansions%ROWTYPE;owner uuid;d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_operation IS NULL OR p_operation NOT IN('derivative.sharpen','export.encode') OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF request IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 owner:=photo.v30_uuid5(photo.v30_uuid5((request->>'child_run_id')::uuid,'workflow'),p_operation||':0');
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND owner_id=owner
 AND purpose='task_params' AND sha256=p_sha AND state='ready';
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=e.tenant_id AND a.id=d.artifact_id
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.output_role='task_params'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('artifact_id',d.artifact_id,'sha256',d.sha256);
END $$;
ALTER FUNCTION photo.v30_delivery_saved_continuation_params(uuid,uuid,integer,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_saved_continuation_params(uuid,uuid,integer,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_saved_continuation_params(uuid,uuid,integer,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
