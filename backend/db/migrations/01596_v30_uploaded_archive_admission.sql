-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_archive_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_artifact uuid,p_sha text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND EXISTS(SELECT 1 FROM photo.projects p JOIN photo.tenants t ON t.id=p.tenant_id WHERE p.tenant_id=p_tenant AND p.id=p_project AND p.archived_at IS NULL AND p.download_allowed AND t.status='active')
 AND EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/zip' AND a.bytes BETWEEN 22 AND 68719476736
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND (EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=a.tenant_id AND l.project_id=p_project AND l.artifact_id=a.id AND l.sha256=a.sha256)
 OR photo.v30_uploaded_archive_ready(p_tenant,p_project,p_actor,p_session,p_artifact,p_sha)));
$$;
ALTER FUNCTION photo.v30_import_archive_ready(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_archive_ready(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_archive_ready(uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- Preserve all existing draft-run checks and replace only source provenance gate.
-- +goose StatementBegin
DO $$ DECLARE definition text;old_predicate constant text:='EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=p_tenant AND l.project_id=project AND l.artifact_id=a.id AND l.sha256=a.sha256)'; BEGIN
 SELECT pg_get_functiondef('photo.begin_v30_interchange_parse_run(uuid,uuid,uuid,text,text,text,text,text)'::regprocedure) INTO definition;
 IF position(old_predicate IN definition)=0 THEN RAISE EXCEPTION 'INTERCHANGE_SOURCE_GATE_NOT_FOUND';END IF;
 EXECUTE replace(definition,old_predicate,'photo.v30_import_archive_ready(p_tenant,project,p_actor,p_session,a.id,a.sha256)');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
