-- +goose Up
-- Every completion entry point must retain the same bound key/receipt that
-- Parts authorized. The predicate was installed by 01798.
-- +goose StatementBegin
DO $$ DECLARE definition text;signature text;needle text;replacement text;
BEGIN
 SELECT pg_get_functiondef('photo.queue_v30_archive_completion(uuid,uuid,uuid,uuid,text,bigint,jsonb)'::regprocedure) INTO definition;
 needle:=' total:=(d.expected_bytes+16777215)/16777216;';
 IF definition IS NULL OR position(needle IN definition)=0 OR position('v30_archive_storage_key_current' IN definition)>0
 THEN RAISE EXCEPTION 'ARCHIVE_GENERATION_QUEUE_SHAPE';END IF;
 EXECUTE replace(definition,needle,
  ' IF photo.v30_archive_storage_key_current(p_tenant,p_upload,d.object_key,d.multipart_id) IS DISTINCT FROM true THEN RETURN false;END IF;'||needle);

 FOREACH signature IN ARRAY ARRAY[
  'photo.claim_v30_archive_completion()',
  'photo.claim_v30_archive_completion_recovery()'
 ] LOOP
  SELECT pg_get_functiondef(signature::regprocedure) INTO definition;
  needle:=' AND photo.v2_editor_authorized(x.tenant_id,x.project_id,x.actor_id,x.completion_session_id)';
  IF definition IS NULL OR position(needle IN definition)=0 OR position('v30_archive_storage_key_current' IN definition)>0
  THEN RAISE EXCEPTION 'ARCHIVE_GENERATION_CLAIM_SHAPE: %',signature;END IF;
  EXECUTE replace(definition,needle,
   ' AND photo.v30_archive_storage_key_current(x.tenant_id,x.id,x.object_key,x.multipart_id)'||needle);
 END LOOP;

 FOREACH signature IN ARRAY ARRAY[
  'photo.start_v30_archive_complete(uuid,uuid,uuid)',
  'photo.recover_v30_archive_complete(uuid,uuid,uuid)',
  'photo.adopt_v30_archive_complete_writer(uuid,uuid,uuid)'
 ] LOOP
  SELECT pg_get_functiondef(signature::regprocedure) INTO definition;
  needle:=' PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=d.project_id AND archived_at IS NULL AND download_allowed FOR SHARE;';
  IF definition IS NULL OR position(needle IN definition)=0 OR position('v30_archive_storage_key_current' IN definition)>0
  THEN RAISE EXCEPTION 'ARCHIVE_GENERATION_WRITER_SHAPE: %',signature;END IF;
  EXECUTE replace(definition,needle,
   ' IF photo.v30_archive_storage_key_current(p_tenant,p_upload,d.object_key,d.multipart_id) IS DISTINCT FROM true THEN RETURN NULL;END IF;'||needle);
 END LOOP;

 SELECT pg_get_functiondef('photo.finish_v30_archive_completion(uuid,uuid,uuid,text,text,text,bigint,text)'::regprocedure) INTO definition;
 needle:=' IF d.state=''verified'' THEN';
 IF definition IS NULL OR position(needle IN definition)=0 OR position('v30_archive_storage_key_current' IN definition)>0
 THEN RAISE EXCEPTION 'ARCHIVE_GENERATION_FINISH_SHAPE';END IF;
 EXECUTE replace(definition,needle,
  ' IF photo.v30_archive_storage_key_current(p_tenant,p_upload,d.object_key,d.multipart_id) IS DISTINCT FROM true THEN RETURN false;END IF;'||needle);
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
