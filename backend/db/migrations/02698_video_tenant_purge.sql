-- +goose Up
-- Add the video tables to the tenant purge allowlist of photo.finish_tenant_deletion (children first).
-- Sequence revisions are immutable; the purge sets the transaction-local flag photo.video_gc='purge'
-- for the duration of the delete loop only. Grants no new rights (photo_fn_owner already holds DELETE).
-- +goose StatementBegin
DO $$
DECLARE definition text; name text;
 entries constant text[]:=ARRAY['video_egress_events','video_egress_counters','video_egress_leases',
  'video_share_budget','video_egress_period','video_tenant_rate','video_tenant_usage_daily','video_tenant_limits',
  'video_collection_items','video_renders','video_job_outputs','video_jobs','video_sequence_sources',
  'video_sequence_revisions','video_sequences','video_media','video_upload_parts','video_upload_sessions'];
 anchor constant text:='tables_to_purge constant text[]:=ARRAY[';
 loop_head constant text:=' FOREACH t IN ARRAY tables_to_purge LOOP';
 loop_tail constant text:='USING j.tenant_id;END LOOP;';
 prefix text:='';
BEGIN
 definition:=pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure);
 IF definition IS NULL
  OR (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1
  OR (length(definition)-length(replace(definition,loop_head,'')))/length(loop_head)<>1
  OR (length(definition)-length(replace(definition,loop_tail,'')))/length(loop_tail)<>1 THEN
  RAISE EXCEPTION 'VIDEO_TENANT_PURGE_SHAPE';
 END IF;
 FOREACH name IN ARRAY entries LOOP
  IF NOT has_table_privilege('photo_fn_owner',format('photo.%I',name),'DELETE') THEN
   RAISE EXCEPTION 'VIDEO_TENANT_PURGE_ENTRY_SHAPE: %',name;
  END IF;
  prefix:=prefix||quote_literal(name)||',';
 END LOOP;
 definition:=replace(definition,anchor,anchor||prefix);
 definition:=replace(definition,loop_head,' PERFORM set_config(''photo.video_gc'',''purge'',true);'||loop_head);
 definition:=replace(definition,loop_tail,loop_tail||' PERFORM set_config(''photo.video_gc'','''',true);');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_tenant_deletion(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_tenant_deletion(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_tenant_deletion(uuid,uuid) TO photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
