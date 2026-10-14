-- +goose Up
-- Private next-watermark scope uses the current canonical presentation producer.
-- It creates no task, publication, adoption receipt or cursor update.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.v30_watermark_initial_stage_scope(uuid,uuid,integer,text,text)'::regprocedure);
 FOR needle,replacement IN SELECT * FROM (VALUES
 ('photo.v30_watermark_initial_stage_scope(','photo.v30_watermark_after_presentation_scope('),
 ('DECLARE request jsonb;','DECLARE producer jsonb;image_ref jsonb;previous text;predecessor uuid;stage_index integer;child photo.runs%ROWTYPE;request jsonb;'),
 (' OR cfg ? ''size'' OR cfg ? ''sharpen''',''),
 ('request->''binding''->''source''->>''sha256''','image_ref->>''sha256'''),
 ('''image'',request->''binding''->''source''','''image'',image_ref')
 ) patches LOOP
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_PRESENTATION_SCOPE_DRIFT: %',needle;END IF;
  definition:=replace(definition,needle,replacement);
 END LOOP;
 needle:=' IF jsonb_typeof(cfg->''watermark'')';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'WATERMARK_PRESENTATION_PRODUCER_DRIFT';END IF;
 definition:=replace(definition,needle,$patch$
 IF cfg->'size' IS NULL AND cfg->'sharpen' IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO child FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(request->>'child_run_id')::uuid FOR UPDATE;
 IF NOT FOUND OR child.status<>'running' OR child.cancel_requested
 OR child.config_snapshot->'v30_delivery_export' IS DISTINCT FROM request THEN RETURN NULL;END IF;
 previous:=CASE WHEN cfg->'sharpen' IS NOT NULL THEN 'derivative.sharpen' ELSE 'derivative.resize' END;
 stage_index:=CASE WHEN cfg->'size' IS NOT NULL THEN 1 ELSE 0 END+CASE WHEN cfg->'sharpen' IS NOT NULL THEN 1 ELSE 0 END;
 predecessor:=photo.v30_uuid5(photo.v30_uuid5(child.id,'workflow'),previous||':0');
 producer:=photo.v30_delivery_stage_result(p_expansion,p_token,p_slot,previous);
 IF producer IS NULL OR producer->>'task_id' IS DISTINCT FROM predecessor::text
 OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v30-stage-verified'
 AND event_id=photo.v2_adoption_event_id(predecessor,(producer->>'result_id')::uuid)) THEN RETURN NULL;END IF;
 SELECT value->'ref' INTO image_ref FROM jsonb_array_elements(producer->'outputs') WHERE value->>'role'='working_image';
 IF image_ref IS NULL THEN RETURN NULL;END IF;
 IF stage_index=2 AND NOT EXISTS(SELECT 1 FROM photo.task_edges edge JOIN photo.tasks resize
 ON resize.tenant_id=edge.tenant_id AND resize.run_id=edge.run_id AND resize.id=edge.prerequisite_id
 WHERE edge.tenant_id=e.tenant_id AND edge.run_id=child.id AND edge.dependent_id=predecessor
 AND edge.condition='success' AND resize.id=photo.v30_uuid5(photo.v30_uuid5(child.id,'workflow'),'derivative.resize:0')
 AND resize.state='succeeded' AND NOT resize.cancel_requested AND resize.canonical_result_id IS NOT NULL
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(resize.id,resize.canonical_result_id))) THEN RETURN NULL;END IF;
 IF jsonb_typeof(cfg->'watermark')
$patch$);
 needle:='''bytes'',octet_length(p_params),''execution_profile'',profile)';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'WATERMARK_PRESENTATION_RETURN_DRIFT';END IF;
 definition:=replace(definition,needle,'''bytes'',octet_length(p_params),''execution_profile'',profile,''predecessor'',predecessor,''stage'',stage_index)');
 EXECUTE definition;
END $$;
ALTER FUNCTION photo.v30_watermark_after_presentation_scope(uuid,uuid,integer,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_after_presentation_scope(uuid,uuid,integer,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
