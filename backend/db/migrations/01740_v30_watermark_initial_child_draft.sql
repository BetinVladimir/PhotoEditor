-- +goose Up
-- Dedicated initial watermark shell, preserving current queue/metadata/quota
-- semantics. No activation, input registration or cursor advancement occurs.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;start_at integer;end_at integer;
BEGIN
 definition:=pg_get_functiondef('photo.create_v30_delivery_child_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint)'::regprocedure);
 FOR needle,replacement IN SELECT * FROM (VALUES
  ('photo.create_v30_delivery_child_draft(','photo.create_v30_watermark_initial_child_draft('),
  ('p_reserved bigint)','p_reserved bigint, p_image text)'),
  ('DECLARE expected jsonb;','DECLARE stage_scope jsonb;expected jsonb;')
 ) patches LOOP
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_CHILD_DRAFT_DEFINITION_DRIFT: %',needle;END IF;
  definition:=replace(definition,needle,replacement);
 END LOOP;
 start_at:=strpos(definition,' IF cfg->''size'' IS NOT NULL THEN');
 end_at:=strpos(definition,' task_id:=photo.v30_uuid5(workflow,operation||'':0'');');
 IF start_at=0 OR end_at<=start_at THEN RAISE EXCEPTION 'WATERMARK_CHILD_DRAFT_STAGE_DRIFT';END IF;
 replacement:=$patch$
 stage_scope:=photo.v30_watermark_initial_params_scope(p_expansion,p_token,p_slot,p_params,p_image);
 IF stage_scope IS NULL OR p_task->'execution_profile' IS DISTINCT FROM stage_scope->'execution_profile' THEN RETURN false;END IF;
 operation:='derivative.watermark';expected_params:=params;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a
 ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=e.tenant_id AND d.run_id=e.run_id AND d.owner_id=(stage_scope->>'owner')::uuid
 AND d.purpose='task_params' AND d.state='ready' AND d.artifact_id=(p_task->>'params_artifact_id')::uuid
 AND d.sha256=stage_scope->>'sha' AND d.bytes=(stage_scope->>'bytes')::bigint
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
$patch$;
 definition:=substring(definition FROM 1 FOR start_at-1)||replacement||substring(definition FROM end_at);
 EXECUTE definition;
END $$;
ALTER FUNCTION photo.create_v30_watermark_initial_child_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v30_watermark_initial_child_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.create_v30_watermark_initial_child_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
