-- +goose Up
-- A typed physical read witness cannot terminate an arbitrary tenant build.
-- Rebind it to the exact live final-render inventory before recording failure.
ALTER TABLE photo.editor_effect_builds ADD COLUMN final_source_failure jsonb
 CHECK(final_source_failure IS NULL OR jsonb_typeof(final_source_failure)='object'
  AND octet_length(final_source_failure::text)<=16384);
-- +goose StatementBegin
CREATE FUNCTION photo.guard_editor_final_source_failure() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.final_source_failure IS NOT NULL THEN RAISE EXCEPTION 'EDITOR_SOURCE_FAILURE_REQUIRES_CURRENT_AUTHORITY' USING ERRCODE='42501';END IF;
 ELSIF NEW.final_source_failure IS DISTINCT FROM OLD.final_source_failure
  AND (current_user<>'photo_fn_owner' OR OLD.final_source_failure IS NOT NULL) THEN
  RAISE EXCEPTION 'EDITOR_SOURCE_FAILURE_IMMUTABLE' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_editor_final_source_failure() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_editor_final_source_failure() FROM PUBLIC;
CREATE TRIGGER editor_final_source_failure_guard BEFORE INSERT OR UPDATE OF final_source_failure
 ON photo.editor_effect_builds FOR EACH ROW EXECUTE FUNCTION photo.guard_editor_final_source_failure();

CREATE FUNCTION photo.fail_editor_effect_final_source(p_tenant uuid,p_build uuid,
 p_bucket text,p_key text,p_version text,p_sha text,p_bytes bigint,p_media text,p_code text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;bound photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_build IS NULL OR p_bucket IS NULL OR p_bucket=''
 OR p_key IS NULL OR octet_length(p_key) NOT BETWEEN 1 AND 2048
 OR p_version IS NULL OR octet_length(p_version) NOT BETWEEN 1 AND 1024 OR p_version='null'
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR p_bytes<1
 OR p_media IS NULL OR octet_length(p_media) NOT BETWEEN 1 AND 200
 OR p_code IS NULL OR p_code NOT IN('EDITOR_SOURCE_OBJECT_MISSING','EDITOR_SOURCE_OBJECT_INTEGRITY') THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM run_id FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_build FOR UPDATE;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 IF b.run_id IS NULL OR b.state NOT IN('sealed','composing') OR b.final_source_failure IS NOT NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id IN(b.run_id,b.seal_run_id,b.final_run_id) ORDER BY id FOR UPDATE;
 IF NOT photo.editor_effect_final_origin_ready(p_tenant,p_build) THEN RETURN false;END IF;
 -- The scheduler reader has no authority to invent an artifact ID. Resolve
 -- the exact immutable descriptor again and prove its current scoped ancestry.
 SELECT a.* INTO bound FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.bucket=p_bucket AND a.object_key=p_key
  AND a.object_version=p_version AND a.sha256=p_sha AND a.bytes=p_bytes AND a.media_type=p_media
  AND EXISTS(SELECT FROM photo.editor_effect_final_artifact(p_tenant,p_build,a.id,a.sha256) authorized
   WHERE authorized.bucket=a.bucket AND authorized.object_key=a.object_key
    AND authorized.object_version=a.object_version AND authorized.bytes=a.bytes AND authorized.media_type=a.media_type)
 ORDER BY a.id LIMIT 1 FOR SHARE OF a;
 IF bound.id IS NULL OR NOT photo.editor_effect_final_origin_ready(p_tenant,p_build) THEN RETURN false;END IF;
 UPDATE photo.editor_effect_builds SET final_source_failure=jsonb_build_object(
  'code',p_code,'artifact_id',bound.id,'sha256',p_sha,'object_version',p_version,
  'object_key',p_key,'bucket',p_bucket,'bytes',p_bytes,'media_type',p_media),updated_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND run_id=p_build AND final_source_failure IS NULL AND state IN('sealed','composing');
 IF NOT FOUND THEN RETURN false;END IF;
 UPDATE photo.runs SET errors=errors||jsonb_build_array(jsonb_build_object('code',p_code,
  'message','An exact editor source object could not be verified. Start a new render from the original source.',
  'artifact_id',bound.id,'sha256',p_sha,'object_version',p_version))
 WHERE tenant_id=p_tenant AND id=p_build;
 RETURN true;
END $$;
ALTER FUNCTION photo.fail_editor_effect_final_source(uuid,uuid,text,text,text,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_editor_effect_final_source(uuid,uuid,text,text,text,text,bigint,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.fail_editor_effect_final_source(uuid,uuid,text,text,text,text,bigint,text,text) TO photo_scheduler;

DO $$ DECLARE definition text;needle text;BEGIN
 definition:=pg_get_functiondef('photo.editor_effect_final_origin_ready(uuid,uuid)'::regprocedure);
 needle:='b.state IN(''sealed'',''composing'',''complete'')';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'EDITOR_PHYSICAL_ORIGIN_ANCHOR';END IF;
 EXECUTE replace(definition,needle,needle||' AND b.final_source_failure IS NULL');
 definition:=pg_get_functiondef('photo.withdraw_editor_effect_build()'::regprocedure);
 needle:='NOT photo.v2_editor_authorized(o.tenant_id,o.project_id,o.actor_id,o.session_id)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'EDITOR_PHYSICAL_DISCOVERY_ANCHOR';END IF;
 definition:=replace(definition,needle,needle||' OR o.final_source_failure IS NOT NULL');
 needle:='IF NOT photo.editor_effect_intermediate_catalog_lost(b.tenant_id,b.run_id) AND photo.v2_editor_authorized(b.tenant_id,b.project_id,b.actor_id,b.session_id)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'EDITOR_PHYSICAL_RECHECK_ANCHOR';END IF;
 EXECUTE replace(definition,needle,'IF b.final_source_failure IS NULL AND NOT photo.editor_effect_intermediate_catalog_lost(b.tenant_id,b.run_id) AND photo.v2_editor_authorized(b.tenant_id,b.project_id,b.actor_id,b.session_id)');
END $$;
-- Existing withdrawal requests cancellation and waits for all live tasks before
-- failure/quota settlement. No object deletion or historical source rewrite.
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
