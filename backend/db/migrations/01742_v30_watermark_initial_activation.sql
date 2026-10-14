-- +goose Up
-- Dedicated watermark activation/cursor admission keeps the current shared
-- queue and metadata predicates, without broadening ordinary child admission.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.commit_v30_delivery_child(uuid,uuid,integer,text,uuid)'::regprocedure);
 FOR needle,replacement IN SELECT * FROM (VALUES
 ('photo.commit_v30_delivery_child(','photo.commit_v30_watermark_initial_child('),
 ('ct.operation IN(''derivative.resize'',''derivative.sharpen'',''export.encode'')','ct.operation=''derivative.watermark''')
 ) patches LOOP
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_CURSOR_DEFINITION_DRIFT: %',needle;END IF;
  definition:=replace(definition,needle,replacement);
 END LOOP;
 needle:=' request:=p_request::jsonb;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_CURSOR_SOURCE_DRIFT';END IF;
 definition:=replace(definition,needle,$patch$
 IF photo.lock_v30_watermark_stage_source(p_expansion,p_token,p_slot) IS DISTINCT FROM true THEN RETURN false;END IF;
 request:=p_request::jsonb;
$patch$);
 EXECUTE definition;
 definition:=pg_get_functiondef('photo.activate_v30_delivery_child(uuid,uuid,integer,text,uuid)'::regprocedure);
 FOR needle,replacement IN SELECT * FROM (VALUES
 ('photo.activate_v30_delivery_child(','photo.activate_v30_watermark_initial_child('),
 ('p_export uuid)','p_export uuid, p_params text, p_image text)'),
 ('DECLARE expected jsonb;','DECLARE stage_scope jsonb;expected jsonb;'),
 ('photo.commit_v30_delivery_child(','photo.commit_v30_watermark_initial_child('),
 ('operation IN(''derivative.resize'',''derivative.sharpen'',''export.encode'')','operation=''derivative.watermark''')
 ) patches LOOP
  -- The cursor call occurs twice: replay and initial admission.
  IF strpos(definition,needle)=0 OR (needle<>'photo.commit_v30_delivery_child(' AND
   (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1) THEN RAISE EXCEPTION 'WATERMARK_ACTIVATION_DEFINITION_DRIFT: %',needle;END IF;
  definition:=replace(definition,needle,replacement);
 END LOOP;
 needle:=' -- For an acknowledgement retry the cursor writer verifies the saved binding.';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'WATERMARK_ACTIVATION_SOURCE_DRIFT';END IF;
 definition:=replace(definition,needle,$patch$
 stage_scope:=photo.v30_watermark_initial_params_scope(p_expansion,p_token,p_slot,p_params,p_image);
 IF stage_scope IS NULL OR NOT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.backend_documents d
 ON d.tenant_id=t.tenant_id AND d.artifact_id=t.params_artifact_id AND d.run_id=e.run_id
 AND d.owner_id=t.id AND d.purpose='task_params' AND d.state='ready'
 WHERE t.tenant_id=e.tenant_id AND t.run_id=child.id AND t.id=(stage_scope->>'owner')::uuid
 AND t.operation='derivative.watermark' AND d.sha256=stage_scope->>'sha'
 AND d.bytes=(stage_scope->>'bytes')::bigint AND photo.v2_input_closure_ready(t.id)
 AND photo.v30_delivery_profile_matches(t.execution_profile,stage_scope->'execution_profile')) THEN RETURN false;END IF;
 -- For an acknowledgement retry the cursor writer verifies the saved binding.
$patch$);
 EXECUTE definition;
END $$;
ALTER FUNCTION photo.commit_v30_watermark_initial_child(uuid,uuid,integer,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_v30_watermark_initial_child(uuid,uuid,integer,text,uuid) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;
ALTER FUNCTION photo.activate_v30_watermark_initial_child(uuid,uuid,integer,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_watermark_initial_child(uuid,uuid,integer,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.activate_v30_watermark_initial_child(uuid,uuid,integer,text,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
