-- +goose Up
-- Local preview queue is selected inside the same tenant-serialized Begin.
-- +goose StatementBegin
CREATE FUNCTION photo.v32_local_portrait_queue(p_request jsonb)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant uuid;run uuid;saved photo.runs%ROWTYPE;
BEGIN
 IF jsonb_typeof(p_request) IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
 tenant:=(p_request->>'tenant_id')::uuid;run:=(p_request->>'run_id')::uuid;
 IF tenant IS NULL OR run IS NULL THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=tenant AND status='active' FOR UPDATE;
 IF NOT FOUND OR photo.v28_local_owner_request_scope(p_request) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO saved FROM photo.runs WHERE id=run FOR SHARE;
 IF NOT FOUND THEN RETURN 'interactive';END IF;
 IF saved.tenant_id IS DISTINCT FROM tenant OR saved.kind IS DISTINCT FROM 'v2'
 OR saved.created_by::text IS DISTINCT FROM p_request->>'actor_id'
 OR saved.config_snapshot->'v28_local_portrait' IS DISTINCT FROM p_request THEN RETURN NULL;END IF;
 IF NOT(saved.config_snapshot ? 'queue_class') THEN RETURN '';END IF;
 IF saved.config_snapshot->'queue_class' IS DISTINCT FROM '"interactive"'::jsonb THEN RETURN NULL;END IF;
 RETURN 'interactive';
EXCEPTION WHEN invalid_text_representation THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v32_local_portrait_queue(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v32_local_portrait_queue(jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v32_local_portrait_queue(jsonb) TO photo_scheduler;
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.begin_v28_local_portrait_draft(text,text,bigint)'::regprocedure);
 needle:='workflow_id uuid;minimum numeric;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected local queue declaration';END IF;
 definition:=replace(definition,needle,'workflow_id uuid;minimum numeric;queue text;');
 needle:='jsonb_object_keys(config) k';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected local queue root';END IF;
 definition:=replace(definition,needle,'jsonb_object_keys(config-''queue_class'') k');
 needle:=' config_sha:=encode(sha256(convert_to(p_config_jcs,''UTF8'')),''hex'');';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected local queue admission';END IF;
 EXECUTE replace(definition,needle,$patch$
 queue:=photo.v32_local_portrait_queue(request);
 IF queue IS NULL THEN RETURN false;END IF;
 IF queue='interactive' THEN
  IF config->'queue_class' IS DISTINCT FROM '"interactive"'::jsonb THEN RETURN false;END IF;
 ELSE
  IF config ? 'queue_class' THEN RETURN false;END IF;
 END IF;
$patch$||needle);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
