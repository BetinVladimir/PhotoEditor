-- +goose Up
-- One network roundtrip per <=100 slots, preserving existing per-slot authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_observation_page(p_expansion uuid,p_token uuid,p_cursor integer,p_limit integer) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;result jsonb:='[]'::jsonb;observation jsonb;slot integer;end_slot integer;
BEGIN
 IF p_cursor IS NULL OR p_cursor NOT BETWEEN 0 AND 80000 OR p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 100 THEN RETURN NULL;END IF;
 IF photo.v30_delivery_expansion_plan(p_expansion,p_token) IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 IF p_cursor>e.cursor::integer THEN RETURN NULL;END IF;
 end_slot:=least(e.cursor::integer,p_cursor+p_limit);
 IF end_slot>p_cursor THEN
  FOR slot IN p_cursor..end_slot-1 LOOP
   observation:=photo.v30_delivery_export_observation(p_expansion,p_token,slot);
   IF observation IS NULL THEN RETURN NULL;END IF;
   result:=result||jsonb_build_array(jsonb_build_object('slot_index',slot,'export',observation));
  END LOOP;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=p_expansion AND expansion_token=p_token
 AND expansion_lease_until>clock_timestamp() AND state='open') THEN RETURN NULL;END IF;
 RETURN result;
END $$;
ALTER FUNCTION photo.v30_delivery_observation_page(uuid,uuid,integer,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_observation_page(uuid,uuid,integer,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_observation_page(uuid,uuid,integer,integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
