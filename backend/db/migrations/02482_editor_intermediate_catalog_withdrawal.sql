-- +goose Up
-- Confirmed loss of an already adopted immutable intermediate must not leave
-- the build outside both final-context discovery and ordinary cancellation.
-- +goose StatementBegin
CREATE FUNCTION photo.editor_effect_intermediate_catalog_lost(p_tenant uuid,p_build uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT FROM photo.editor_effect_builds b
 WHERE b.tenant_id=p_tenant AND b.run_id=p_build AND b.state IN('sealed','composing')
 AND b.seal_adoption_digest IS NOT NULL AND b.seal_adoption_sql_digest IS NOT NULL
 AND EXISTS(SELECT FROM (VALUES
  (b.seal_receipt_artifact_id,b.seal_receipt_sha),
  (b.result_artifact_id,b.result_sha),
  (b.processed_source_artifact_id,b.processed_source_sha),
  (b.executed_style_artifact_id,b.executed_style_sha),
  (b.quality_artifact_id,b.quality_sha)) required(id,sha)
  WHERE required.id IS NOT NULL AND required.sha IS NOT NULL AND NOT EXISTS(
   SELECT FROM photo.artifacts a WHERE a.tenant_id=b.tenant_id AND a.id=required.id
   AND a.sha256=required.sha AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))));
$$;
ALTER FUNCTION photo.editor_effect_intermediate_catalog_lost(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_intermediate_catalog_lost(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance;

DO $$ DECLARE definition text;needle text;replacement text;BEGIN
 definition:=pg_get_functiondef('photo.withdraw_editor_effect_build()'::regprocedure);
 needle:='NOT photo.v2_editor_authorized(o.tenant_id,o.project_id,o.actor_id,o.session_id)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'EDITOR_INTERMEDIATE_DISCOVERY_ANCHOR';END IF;
 definition:=replace(definition,needle,needle||E'\n  OR photo.editor_effect_intermediate_catalog_lost(o.tenant_id,o.run_id)');
 -- Keep existing tenant/usage/build/run lock order. Lock exact immutable refs
 -- only after the build fence, then repeat the predicate before any write.
 needle:='IF b.state IN(''complete'',''failed'') THEN RETURN false;END IF;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'EDITOR_INTERMEDIATE_LOCK_ANCHOR';END IF;
 replacement:=needle||E'\n PERFORM id FROM photo.runs WHERE tenant_id=b.tenant_id AND id IN(b.run_id,b.seal_run_id,b.final_run_id) ORDER BY id FOR UPDATE;\n PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=b.tenant_id AND a.id IN(b.seal_receipt_artifact_id,b.result_artifact_id,b.processed_source_artifact_id,b.executed_style_artifact_id,b.quality_artifact_id) ORDER BY a.id FOR SHARE;';
 definition:=replace(definition,needle,replacement);
 needle:='IF photo.v2_editor_authorized(b.tenant_id,b.project_id,b.actor_id,b.session_id)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'EDITOR_INTERMEDIATE_RECHECK_ANCHOR';END IF;
 definition:=replace(definition,needle,'IF NOT photo.editor_effect_intermediate_catalog_lost(b.tenant_id,b.run_id) AND photo.v2_editor_authorized(b.tenant_id,b.project_id,b.actor_id,b.session_id)');
 needle:='FOR r IN SELECT * FROM photo.runs WHERE tenant_id=b.tenant_id AND id IN(b.run_id,b.seal_run_id,b.final_run_id) ORDER BY id FOR UPDATE LOOP';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'EDITOR_INTERMEDIATE_DIAGNOSTIC_ANCHOR';END IF;
 replacement:=E'IF photo.editor_effect_intermediate_catalog_lost(b.tenant_id,b.run_id) THEN\n  UPDATE photo.runs SET errors=errors||jsonb_build_array(jsonb_build_object(''code'',''EDITOR_INTERMEDIATE_UNAVAILABLE'',''message'',''An adopted editor intermediate is no longer available. Start a new render from the original source.''))\n  WHERE tenant_id=b.tenant_id AND id=b.run_id AND NOT errors @> ''[{"code":"EDITOR_INTERMEDIATE_UNAVAILABLE"}]''::jsonb;\n END IF;\n '||needle;
 definition:=replace(definition,needle,replacement);
 EXECUTE definition;
END $$;
-- The existing withdrawal function retains its scheduler-only ACL, ordinary
-- child cancellation, live-task drain, quota settlement and build failure.
-- No physical objects, accepted masters or immutable source pins are changed.
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
