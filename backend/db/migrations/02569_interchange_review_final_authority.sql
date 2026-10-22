-- +goose Up
-- Preserve installed entrypoint identities and ACLs; repeat authority after
-- decision locks, including immutable replay, before publishing an epoch.
-- +goose StatementBegin
DO $$
DECLARE target oid; source text; definition text; signature text; marker text;
 guard text; workflow_lock constant text := 'AND workflow_type=''interchange'' AND renderer_version=''v2.2'' FOR SHARE;';
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.grant_v30_import_review(uuid,uuid,uuid,uuid,uuid,text,text,jsonb)',
  'photo.revoke_v30_import_review(uuid,uuid,uuid,uuid,uuid,jsonb)'] LOOP
  target:=to_regprocedure(signature);
  SELECT p.prosrc,pg_get_functiondef(p.oid) INTO source,definition FROM pg_proc p
  WHERE p.oid=target AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
   AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
   AND EXISTS(SELECT 1 FROM unnest(p.proconfig) setting WHERE replace(setting,' ','')='search_path=pg_catalog,photo');
  IF source IS NULL OR strpos(source,'-- 02569:')>0 THEN RAISE EXCEPTION 'INTERCHANGE_REVIEW_FINAL_AUTHORITY_SOURCE'; END IF;
  guard:=$guard$
 -- 02569: IF expressions preserve FOUND from the decision row lookup.
 IF photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
$guard$;
  IF signature LIKE 'photo.grant_%' THEN
   marker:=' IF FOUND THEN'||chr(10)||'  IF old.project_id<>w.project_id';
   IF strpos(source,workflow_lock)=0 OR strpos(source,'FOR SHARE OF b,a;')=0 THEN RAISE EXCEPTION 'INTERCHANGE_REVIEW_GRANT_LOCK_SHAPE';END IF;
   guard:=guard||$guard$
 IF r.status<>'running' OR r.cancel_requested OR NOT EXISTS(
  SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=proposal_id
   AND a.sha256=proposal_sha AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
$guard$;
   -- The existing retry_after write must not introduce a later SHARE upgrade wait.
   source:=replace(source,workflow_lock,replace(workflow_lock,'FOR SHARE;','FOR UPDATE;'));
  ELSE
   marker:=' IF NOT FOUND OR old.proposal_sha<>proposal_sha';
  END IF;
  IF (length(source)-length(replace(source,marker,'')))/length(marker)<>1 THEN RAISE EXCEPTION 'INTERCHANGE_REVIEW_FINAL_AUTHORITY_MARKER';END IF;
  -- Replace original prosrc inside pg_get_functiondef, then install patched body.
  EXECUTE replace(definition,(SELECT prosrc FROM pg_proc WHERE oid=target),replace(source,marker,guard||marker));
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
