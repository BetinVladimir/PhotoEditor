-- +goose Up
CREATE TABLE photo.v25_lens_profile_catalog (
 tenant_id uuid NOT NULL, profile_id uuid NOT NULL, profile_sha text NOT NULL CHECK(profile_sha ~ '^[0-9a-f]{64}$'),
 qualification_id uuid NOT NULL, qualification_sha text NOT NULL CHECK(qualification_sha ~ '^[0-9a-f]{64}$'),
 coefficients_sha text NOT NULL CHECK(coefficients_sha ~ '^[0-9a-f]{64}$'),
 runtime_sha text NOT NULL CHECK(runtime_sha ~ '^[0-9a-f]{64}$'),
 PRIMARY KEY(tenant_id,profile_id),
 FOREIGN KEY(tenant_id,profile_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,qualification_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE photo.v25_lens_profile_catalog ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v25_lens_profile_catalog FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v25_lens_profile_catalog TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,DELETE ON photo.v25_lens_profile_catalog TO photo_fn_owner;
-- +goose StatementBegin
CREATE FUNCTION photo.register_v25_lens_profile(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_profile uuid,p_sha text,p_qualification uuid,p_qualification_sha text,p_coefficients_sha text,p_runtime_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v25_lens_profile_object(p_tenant,p_project,p_actor,p_session,p_profile,p_sha))
 OR NOT EXISTS(SELECT 1 FROM photo.v25_lens_profile_object(p_tenant,p_project,p_actor,p_session,p_qualification,p_qualification_sha))
 OR coalesce(p_coefficients_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(p_runtime_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 -- Registration records discovery only; actual bytes and trusted pins are checked
 -- by the installed service before registration and every catalog read/admission.
 INSERT INTO photo.v25_lens_profile_catalog VALUES(p_tenant,p_profile,p_sha,p_qualification,p_qualification_sha,p_coefficients_sha,p_runtime_sha)
 ON CONFLICT(tenant_id,profile_id) DO NOTHING;
 RETURN EXISTS(SELECT 1 FROM photo.v25_lens_profile_catalog c WHERE c.tenant_id=p_tenant AND c.profile_id=p_profile
 AND c.profile_sha=p_sha AND c.qualification_id=p_qualification AND c.qualification_sha=p_qualification_sha
 AND c.coefficients_sha=p_coefficients_sha AND c.runtime_sha=p_runtime_sha);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_v25_lens_profile(uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v25_lens_profile(uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_v25_lens_profile(uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,text) TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.list_v25_lens_profiles(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_after uuid)
RETURNS TABLE(profile_id uuid,profile_sha text) LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT c.profile_id,c.profile_sha FROM photo.v25_lens_profile_catalog c
 JOIN photo.artifacts a ON a.tenant_id=c.tenant_id AND a.id=c.profile_id AND a.sha256=c.profile_sha
 JOIN photo.artifacts q ON q.tenant_id=c.tenant_id AND q.id=c.qualification_id AND q.sha256=c.qualification_sha
 WHERE c.tenant_id=p_tenant AND (p_after IS NULL OR c.profile_id>p_after)
 AND a.state='ready' AND q.state='ready' AND a.verified_at IS NOT NULL AND q.verified_at IS NOT NULL
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND (q.expires_at IS NULL OR q.expires_at>clock_timestamp())
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 ORDER BY c.profile_id LIMIT 101;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.list_v25_lens_profiles(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.list_v25_lens_profiles(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.list_v25_lens_profiles(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementBegin
DO $$ DECLARE definition text; anchor constant text:='''v29_perspective_lines'','; entry constant text:='''v25_lens_profile_catalog'',';
BEGIN
 definition:=pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure);
 IF strpos(definition,anchor)=0 OR strpos(definition,entry)>0 THEN RAISE EXCEPTION 'V25_LENS_CATALOG_PURGE_SHAPE';END IF;
 EXECUTE replace(definition,anchor,entry||anchor);
END $$;
-- +goose StatementEnd
CREATE TRIGGER ready_artifact_reference BEFORE INSERT ON photo.v25_lens_profile_catalog
 FOR EACH ROW EXECUTE FUNCTION photo.guard_ready_artifact_reference('profile_id','qualification_id');
-- +goose StatementBegin
DO $$ DECLARE body text;updated text;
BEGIN
 SELECT prosrc INTO body FROM pg_proc WHERE oid='photo.v2_artifact_has_references(uuid,uuid)'::regprocedure
 AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql');
 IF body IS NULL OR body !~* '^[[:space:]]*SELECT[[:space:]]' OR strpos(body,'photo.v25_lens_profile_catalog')>0 THEN
 RAISE EXCEPTION 'V25_LENS_CATALOG_GC_SHAPE';END IF;
 updated:=regexp_replace(body,'^([[:space:]]*SELECT[[:space:]]+)',
 E'\\1 EXISTS(SELECT FROM photo.v25_lens_profile_catalog c WHERE c.tenant_id=p_tenant AND p_artifact IN(c.profile_id,c.qualification_id)) OR ','i');
 IF updated=body THEN RAISE EXCEPTION 'V25_LENS_CATALOG_GC_PATCH_FAILED';END IF;
 EXECUTE format('CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS %L',updated);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
