-- +goose Up
-- A terminal label is not proof of physical deletion. Existing malformed
-- rows stay present but are refused by every irreversible purge path.
ALTER TABLE photo.v30_archive_begin_generations
 ADD CONSTRAINT v30_archive_aborted_requires_purge_proof
 CHECK (state<>'aborted' OR purged_at IS NOT NULL) NOT VALID;

-- +goose StatementBegin
DO $$ DECLARE definition text;old_guard text;new_guard text;
BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure) INTO definition;
 old_guard:='g.state IN(''creating'',''created'',''aborting'',''bound'')';
 new_guard:='(g.state<>''aborted'' OR g.purged_at IS NULL)';
 IF definition IS NULL OR position(old_guard IN definition)=0 OR position('v30_archive_begin_generations' IN definition)=0
 THEN RAISE EXCEPTION 'ARCHIVE_TENANT_PURGE_PROOF_SHAPE';END IF;
 EXECUTE replace(definition,old_guard,new_guard);

 FOR definition IN SELECT pg_get_functiondef(oid) FROM pg_proc
  WHERE oid IN ('photo.claim_v30_archive_cleanup()'::regprocedure,
                'photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text)'::regprocedure) LOOP
  old_guard:='g.state<>''aborted''';
  new_guard:='(g.state<>''aborted'' OR g.purged_at IS NULL)';
  IF position(old_guard IN definition)=0 OR position('v30_archive_begin_generations' IN definition)=0
  THEN RAISE EXCEPTION 'ARCHIVE_PARENT_PURGE_PROOF_SHAPE';END IF;
  EXECUTE replace(definition,old_guard,new_guard);
 END LOOP;

 SELECT pg_get_functiondef('photo.v30_archive_cleanup_key_current(uuid,uuid,text,text)'::regprocedure) INTO definition;
 IF definition IS NULL OR position('g.state<>''aborted''' IN definition)=0
  OR position('other.state<>''aborted''' IN definition)=0
 THEN RAISE EXCEPTION 'ARCHIVE_PARENT_KEY_PURGE_PROOF_SHAPE';END IF;
 definition:=replace(definition,'g.state<>''aborted''','(g.state<>''aborted'' OR g.purged_at IS NULL)');
 definition:=replace(definition,'other.state<>''aborted''','(other.state<>''aborted'' OR other.purged_at IS NULL)');
 EXECUTE definition;

 SELECT pg_get_functiondef('photo.finish_v30_archive_cleanup(uuid,uuid,uuid,text,text)'::regprocedure) INTO definition;
 old_guard:='IF d.state=''aborted'' THEN RETURN true;END IF;';
 new_guard:='IF d.state=''aborted'' THEN RETURN NOT EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g
  WHERE g.tenant_id=p_tenant AND g.upload_id=p_upload AND (g.state<>''aborted'' OR g.purged_at IS NULL));END IF;';
 IF definition IS NULL OR position(old_guard IN definition)=0
  OR position('photo.v30_archive_cleanup_authorized(p_tenant,p_upload,p_token,p_bucket,p_key)' IN definition)=0
 THEN RAISE EXCEPTION 'ARCHIVE_PARENT_FINISH_PURGE_PROOF_SHAPE';END IF;
 EXECUTE replace(definition,old_guard,new_guard);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
