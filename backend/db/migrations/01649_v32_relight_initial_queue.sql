-- +goose Up
-- Queue selection is authorized and serialized with draft creation.
-- +goose StatementBegin
CREATE FUNCTION photo.v32_relight_initial_queue(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_run uuid)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved photo.runs%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_run IS NULL
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO saved FROM photo.runs WHERE id=p_run FOR SHARE;
 IF NOT FOUND THEN RETURN 'interactive';END IF;
 IF saved.tenant_id IS DISTINCT FROM p_tenant OR saved.created_by IS DISTINCT FROM p_actor
 OR saved.kind IS DISTINCT FROM 'v2' OR saved.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text
 OR NOT(saved.config_snapshot ? 'v28_relight_request') THEN RETURN NULL;END IF;
 IF NOT(saved.config_snapshot ? 'queue_class') THEN RETURN '';END IF;
 IF saved.config_snapshot->'queue_class' IS DISTINCT FROM '"interactive"'::jsonb THEN RETURN NULL;END IF;
 RETURN 'interactive';
END $$;
ALTER FUNCTION photo.v32_relight_initial_queue(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v32_relight_initial_queue(uuid,uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v32_relight_initial_queue(uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;
DO $$
DECLARE definition text; needle text;
BEGIN
 definition:=pg_get_functiondef('photo.create_v28_relight_initial_draft(uuid,uuid,uuid,uuid,text,jsonb,text)'::regprocedure);
 needle:='h bytea;expected uuid;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected relight initial declaration'; END IF;
 definition:=replace(definition,needle,'h bytea;expected uuid;queue text;');
 needle:=' IF p_config IS NULL OR octet_length(p_config)>262144 OR p_config::jsonb IS DISTINCT FROM config THEN RETURN false;END IF;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected relight initial queue definition'; END IF;
 EXECUTE replace(definition,needle,$patch$
 queue:=photo.v32_relight_initial_queue(p_tenant,p_project,p_actor,p_session,v_run);
 IF queue IS NULL THEN RETURN false;END IF;
 IF queue<>'' THEN config:=config||jsonb_build_object('queue_class',queue);END IF;
 IF p_config IS NULL OR octet_length(p_config)>262144 OR p_config::jsonb IS DISTINCT FROM config THEN RETURN false;END IF;
$patch$);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
