-- +goose Up
-- Preserve the installed function identity, owner, grants and surrounding checks.
-- +goose StatementBegin
DO $$
DECLARE target oid:=to_regprocedure('photo.finish_v22_analysis_apply(uuid,uuid,uuid,uuid,bigint,text,uuid,text,text,uuid[],uuid)');
 source text; definition text;
 marker constant text := ' INSERT INTO photo.v22_analysis_decisions(tenant_id,project_id,analysis_id,';
 guard constant text := $guard$
 -- 02567: acquire the publication cursor before the final live authority read.
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0)
  ON CONFLICT(tenant_id) DO NOTHING;
 PERFORM 1 FROM photo.tenant_event_cursors
  WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(
  p_tenant,analysis.project_id,p_actor,p_auth_session) THEN
  RAISE EXCEPTION 'V22_APPLY_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 -- All parent/child/cursor waits precede this final publication authority fence.
$guard$;
BEGIN
 SELECT p.prosrc,pg_get_functiondef(p.oid) INTO source,definition FROM pg_proc p
 WHERE p.oid=target AND p.prosecdef
 AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 AND EXISTS(SELECT 1 FROM unnest(p.proconfig) setting
  WHERE replace(setting,' ','')='search_path=pg_catalog,photo');
 IF source IS NULL OR strpos(source,'-- 02567:')>0
 OR strpos(source,'parent.revision<>p_expected_revision')=0
 OR strpos(source,'AND r.id=p_child FOR UPDATE;')=0
 OR strpos(source,'last_cursor=last_cursor+1')=0
 OR (length(source)-length(replace(source,marker,'')))/length(marker)<>1 THEN
  RAISE EXCEPTION 'V22_APPLY_FINAL_AUTHORITY_SOURCE_SHAPE';
 END IF;
 EXECUTE replace(definition,source,replace(source,marker,guard||marker));
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
