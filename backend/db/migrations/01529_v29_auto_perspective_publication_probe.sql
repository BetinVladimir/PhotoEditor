-- +goose Up
-- Explicit absence/draft/admission observation; no repair or quota renewal.
-- +goose StatementBegin
CREATE FUNCTION photo.probe_v29_auto_perspective_publication(p_tenant uuid,p_expected text,p_segments jsonb,p_bucket text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE draft jsonb;run uuid;r photo.runs%ROWTYPE;
BEGIN
 IF photo.lock_v29_auto_perspective_replacement_identity(p_tenant,p_expected,p_segments,p_bucket) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 draft:=p_expected::jsonb;run:=(draft->'identity'->>'run_id')::uuid;
 SELECT * INTO r FROM photo.runs WHERE id=run FOR SHARE;
 IF NOT FOUND THEN RETURN 'absent';END IF;
 IF r.tenant_id IS DISTINCT FROM p_tenant OR r.cancel_requested THEN RETURN NULL;END IF;
 IF r.graph_sealed THEN
 IF photo.read_v29_auto_perspective_replacement_queued_receipt(p_tenant,run,(draft->'draft'->>'actor_id')::uuid,
 (draft->'draft'->>'auth_session_id')::uuid,p_expected,p_segments,p_bucket) IS NULL THEN RETURN NULL;END IF;
 RETURN 'admitted';END IF;
 IF photo.read_v29_auto_perspective_replacement_draft(p_tenant,p_expected,p_segments,p_bucket) IS DISTINCT FROM p_expected THEN RETURN NULL;END IF;
 RETURN 'draft';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.probe_v29_auto_perspective_publication(uuid,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.probe_v29_auto_perspective_publication(uuid,text,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.probe_v29_auto_perspective_publication(uuid,text,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
