-- +goose Up
-- Archive publication must use the current producer attempt, not merely a
-- canonical_result_id left behind by an earlier lease. Lock the complete
-- producer tuple while the two-phase finalizer validates and publishes.
-- +goose StatementBegin
DO $$
DECLARE definition text;old_join text;new_join text;old_where text;new_where text;
BEGIN
 SELECT pg_get_functiondef('photo.finalize_download_archives(uuid,uuid)'::regprocedure) INTO definition;
 old_join:='z.id=t.canonical_result_id';
 new_join:='z.id=t.canonical_result_id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token';
 old_where:='t.tenant_id=p_tenant AND t.id=v.task_id AND t.state=''succeeded''';
 new_where:='t.tenant_id=p_tenant AND t.id=v.task_id AND t.run_id=p_run AND t.operation=''archive.build'' AND t.state=''succeeded''';
 IF definition IS NULL
  OR (length(definition)-length(replace(definition,old_join,'')))/length(old_join)<>4
  OR (length(definition)-length(replace(definition,old_where,'')))/length(old_where)<>4
  OR position('z.attempt=t.attempt' IN definition)>0
  OR position('Phase one' IN definition)=0 OR position('Phase two' IN definition)=0 THEN
  RAISE EXCEPTION 'DOWNLOAD_ARCHIVE_CURRENT_ATTEMPT_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,old_join,new_join);
 definition:=replace(definition,old_where,new_where);
 -- Both role lookups must retain immutable ready objects for the whole commit.
 old_join:='a.output_role=''archive'' AND a.state=''ready''';
 new_join:=old_join||' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())';
 IF (length(definition)-length(replace(definition,old_join,'')))/length(old_join)<>2 THEN
  RAISE EXCEPTION 'DOWNLOAD_ARCHIVE_OUTPUT_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,old_join,new_join);
 old_join:='a.output_role=''archive_manifest'' AND a.state=''ready''';
 new_join:=old_join||' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())';
 IF (length(definition)-length(replace(definition,old_join,'')))/length(old_join)<>2 THEN
  RAISE EXCEPTION 'DOWNLOAD_ARCHIVE_MANIFEST_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,old_join,new_join);
 old_join:='z.outputs @> jsonb_build_array(jsonb_build_object(''artifact_id'',a.id::text,''sha256'',a.sha256));';
 new_join:='z.outputs @> jsonb_build_array(jsonb_build_object(''artifact_id'',a.id::text,''sha256'',a.sha256)) FOR SHARE OF t,z,a;';
 IF (length(definition)-length(replace(definition,old_join,'')))/length(old_join)<>4 THEN
  RAISE EXCEPTION 'DOWNLOAD_ARCHIVE_PRODUCER_LOCK_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,old_join,new_join);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_download_archives(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_download_archives(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_download_archives(uuid,uuid) TO photo_scheduler;
-- +goose StatementBegin
DO $$
DECLARE definition text;
 anchor text := E'IF NOT FOUND THEN RETURN false;END IF;\n  RETURN a.object_key IS NOT DISTINCT FROM p_object_key';
 replacement text := $patch$IF NOT FOUND THEN RETURN false;END IF;
  IF i.intent_kind='volume' THEN
   IF NOT EXISTS(SELECT FROM photo.tasks task
    JOIN photo.task_results result ON result.tenant_id=task.tenant_id
     AND result.task_id=task.id AND result.id=task.canonical_result_id
     AND result.attempt=task.attempt AND result.fencing_token=task.fencing_token
    WHERE task.tenant_id=i.tenant_id AND task.id=a.created_by_task_id
     AND task.run_id=j.run_id AND task.state='succeeded'
     AND ((task.operation='archive.build' AND a.output_role='archive')
      OR (task.operation='export.encode' AND a.output_role='encoded_image'))
     AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
    FOR SHARE OF task,result) THEN RETURN false;END IF;
   IF a.output_role='archive' AND NOT EXISTS(SELECT FROM photo.artifacts manifest
    JOIN photo.tasks task ON task.tenant_id=manifest.tenant_id AND task.id=manifest.created_by_task_id
    JOIN photo.task_results result ON result.tenant_id=task.tenant_id AND result.task_id=task.id
     AND result.id=task.canonical_result_id AND result.attempt=task.attempt
     AND result.fencing_token=task.fencing_token
    WHERE manifest.tenant_id=i.tenant_id AND manifest.id=v.manifest_artifact_id
     AND manifest.created_by_task_id=a.created_by_task_id AND manifest.output_role='archive_manifest'
     AND manifest.state='ready' AND manifest.expires_at>clock_timestamp()
     AND manifest.object_version IS NOT NULL AND manifest.object_version NOT IN ('','null')
     AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',manifest.id::text,'sha256',manifest.sha256))
    FOR SHARE OF manifest,task,result) THEN RETURN false;END IF;
  END IF;
  RETURN a.object_key IS NOT DISTINCT FROM p_object_key$patch$;
BEGIN
 SELECT pg_get_functiondef('photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)'::regprocedure) INTO definition;
 IF definition IS NULL OR
  (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN
  RAISE EXCEPTION 'DOWNLOAD_VOLUME_POSTSIGN_PRODUCER_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,anchor,replacement);
 EXECUTE definition;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE definition text;
 anchor text := 'SELECT count(DISTINCT volume_no) INTO zip_count';
 replacement text := $patch$-- Direct-file volumes have no archive worker; recheck their encode producer
 -- before marking the whole job ready, including mixed ZIP/direct jobs.
 IF EXISTS(SELECT FROM photo.download_volumes volume
  WHERE volume.tenant_id=p_tenant AND volume.download_job_id=j.id AND volume.status='ready'
   AND NOT EXISTS(SELECT FROM photo.artifacts artifact
    JOIN photo.tasks task ON task.tenant_id=artifact.tenant_id AND task.id=artifact.created_by_task_id
    JOIN photo.task_results result ON result.tenant_id=task.tenant_id AND result.task_id=task.id
     AND result.id=task.canonical_result_id AND result.attempt=task.attempt
     AND result.fencing_token=task.fencing_token
    WHERE artifact.tenant_id=volume.tenant_id AND artifact.id=volume.artifact_id
     AND artifact.state='ready' AND artifact.output_role='encoded_image'
     AND (artifact.expires_at IS NULL OR artifact.expires_at>clock_timestamp())
     AND artifact.object_version IS NOT NULL AND artifact.object_version NOT IN ('','null')
     AND task.run_id=p_run AND task.operation='export.encode' AND task.state='succeeded'
     AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',artifact.id::text,'sha256',artifact.sha256))
    FOR SHARE OF artifact,task,result)) THEN RETURN false;END IF;
 SELECT count(DISTINCT volume_no) INTO zip_count$patch$;
BEGIN
 SELECT pg_get_functiondef('photo.seal_download_archives(uuid,uuid)'::regprocedure) INTO definition;
 IF definition IS NULL OR
  (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1
  OR position('IF zip_count=0 THEN' IN definition)=0 THEN
  RAISE EXCEPTION 'DOWNLOAD_DIRECT_SEAL_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,anchor,replacement);
 EXECUTE definition;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE signature text;definition text;
 anchor text := 'RETURN QUERY SELECT ''replayed'',existing.id,existing.expires_at;RETURN;';
 replacement text := $patch$IF existing.consumed_at IS NOT NULL OR existing.expires_at<=clock_timestamp() THEN
  RETURN QUERY SELECT 'not_ready',NULL::uuid,NULL::timestamptz;RETURN;
 END IF;
 RETURN QUERY SELECT 'replayed',existing.id,existing.expires_at;RETURN;$patch$;
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.create_portal_download_intent(bytea,bytea,text,text,uuid,integer,uuid,bytea,timestamptz)',
  'photo.create_portal_item_download_intent(bytea,bytea,text,text,uuid,uuid,uuid,bytea,timestamptz)'
 ] LOOP
  SELECT pg_get_functiondef(signature::regprocedure) INTO definition;
  IF definition IS NULL OR
   (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN
   RAISE EXCEPTION 'GUEST_DOWNLOAD_REPLAY_SOURCE_SHAPE: %',signature;
  END IF;
  EXECUTE replace(definition,anchor,replacement);
 END LOOP;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE signature text;definition text;
 anchor text := 'z.id=t.canonical_result_id';
 replacement text := 'z.id=t.canonical_result_id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token';
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.download_encode_outcomes(uuid,uuid)',
  'photo.download_archive_context(uuid,uuid)'
 ] LOOP
  SELECT pg_get_functiondef(signature::regprocedure) INTO definition;
  IF definition IS NULL OR
   (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1
   OR position('z.attempt=t.attempt' IN definition)>0 THEN
   RAISE EXCEPTION 'DOWNLOAD_ENCODE_FENCE_SOURCE_SHAPE: %',signature;
  END IF;
  definition:=replace(definition,anchor,replacement);
  IF signature='photo.download_encode_outcomes(uuid,uuid)' THEN
   anchor:='a.output_role=''encoded_image'' AND a.state=''ready''';
   IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN
    RAISE EXCEPTION 'DOWNLOAD_ENCODE_EXPIRY_SOURCE_SHAPE';
   END IF;
   definition:=replace(definition,anchor,anchor||' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())');
   anchor:='z.id=t.canonical_result_id';
  END IF;
  EXECUTE definition;
 END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
