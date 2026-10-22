-- Work-only02597 candidate. Task authorized; execution awaits coordinated
-- main-run terminal/cleanup and source-slot release. Error names only.
-- +goose Up
-- +goose StatementBegin
DO $repair$
DECLARE target record; before_row pg_proc%ROWTYPE; after_row pg_proc%ROWTYPE; definition text; next_source text;
BEGIN
 FOR target IN SELECT * FROM (VALUES
(1,'photo.commit_task_success_before_background_cleanup_bundle(uuid,integer,bigint,uuid,text)','fa396119724daa14118dddb1656c358ccf11e8cff44b9b62cf00a423138fd10e',1,0),
(2,'photo.commit_task_success_before_retouch_bundle(uuid,integer,bigint,uuid,text)','7866221eb77f6ead1a6d67f7c683fc4b25fd70e5e8caee9443ddf815c2528ef2',1,1)
 ) v(ordinal,signature,source_sha,stale_count,bundle_count) ORDER BY ordinal LOOP
  SELECT * INTO before_row FROM pg_proc WHERE oid=to_regprocedure(target.signature);
  IF NOT FOUND OR before_row.proowner IS DISTINCT FROM 'photo_fn_owner'::regrole::oid
   OR NOT before_row.prosecdef OR before_row.prokind<>'f' OR NOT before_row.proretset
   OR before_row.provolatile<>'v' OR before_row.prolang IS DISTINCT FROM (SELECT oid FROM pg_language WHERE lanname='plpgsql')
   OR before_row.proargnames IS DISTINCT FROM ARRAY['p_task','p_attempt','p_fence','p_worker','p_manifest','outcome','result_id','result_digest']::text[]
   OR pg_get_function_result(before_row.oid)<>'TABLE(outcome text, result_id uuid, result_digest text)'
   OR before_row.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, photo']::text[]
   OR encode(sha256(convert_to(before_row.prosrc,'UTF8')),'hex')<>target.source_sha
   OR (length(before_row.prosrc)-length(replace(before_row.prosrc,'SEMANTIC_REMOVAL_PREVIEW_RESULT_STALE','')))/length('SEMANTIC_REMOVAL_PREVIEW_RESULT_STALE')<>target.stale_count
   OR (length(before_row.prosrc)-length(replace(before_row.prosrc,'SEMANTIC_REMOVAL_PREVIEW_BUNDLE_INCOMPLETE','')))/length('SEMANTIC_REMOVAL_PREVIEW_BUNDLE_INCOMPLETE')<>target.bundle_count
  THEN RAISE EXCEPTION 'CLEANUP_DIAGNOSTIC_SOURCE_DRIFT'; END IF;
  IF EXISTS(SELECT 1 FROM aclexplode(coalesce(before_row.proacl,acldefault('f',before_row.proowner))) a
   WHERE a.grantee<>before_row.proowner OR a.privilege_type<>'EXECUTE')
   OR EXISTS(SELECT 1 FROM unnest(ARRAY['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal','photo_semantic_admission']) role_name
    WHERE has_function_privilege(role_name,before_row.oid,'EXECUTE'))
  THEN RAISE EXCEPTION 'CLEANUP_DIAGNOSTIC_ACL_DRIFT';END IF;
  next_source:=replace(replace(before_row.prosrc,'SEMANTIC_REMOVAL_PREVIEW_RESULT_STALE','SEMANTIC_BACKGROUND_CLEANUP_PREVIEW_RESULT_STALE'),
   'SEMANTIC_REMOVAL_PREVIEW_BUNDLE_INCOMPLETE','SEMANTIC_BACKGROUND_CLEANUP_PREVIEW_BUNDLE_INCOMPLETE');
  definition:=pg_get_functiondef(before_row.oid);
  IF (length(definition)-length(replace(definition,before_row.prosrc,'')))/length(before_row.prosrc)<>1 THEN RAISE EXCEPTION 'CLEANUP_DIAGNOSTIC_DEFINITION_DRIFT';END IF;
  EXECUTE replace(definition,before_row.prosrc,next_source);
  SELECT * INTO after_row FROM pg_proc WHERE oid=before_row.oid;
  IF NOT FOUND OR to_jsonb(after_row)-'prosrc' IS DISTINCT FROM to_jsonb(before_row)-'prosrc'
   OR after_row.prosrc IS DISTINCT FROM next_source
  THEN RAISE EXCEPTION 'CLEANUP_DIAGNOSTIC_ABI_DRIFT'; END IF;
 END LOOP;
END $repair$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
