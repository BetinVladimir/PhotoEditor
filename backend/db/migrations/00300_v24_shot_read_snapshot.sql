-- +goose Up
-- Keep revision and document pointer coherent without granting API table DML.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v24_shot_snapshot(p_tenant uuid,p_session uuid,p_actor uuid,p_auth_session uuid)
RETURNS TABLE(project_id uuid,revision bigint,document_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s photo.shoot_sessions%ROWTYPE; member_role text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN
  RAISE EXCEPTION 'V24_SHOT_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 SELECT a.role INTO member_role FROM photo.authorize_tenant(p_actor,p_auth_session,p_tenant) a;
 IF member_role IS NULL THEN RAISE EXCEPTION 'V24_SHOT_FORBIDDEN' USING ERRCODE='42501'; END IF;
 SELECT x.* INTO s FROM photo.shoot_sessions x WHERE x.tenant_id=p_tenant AND x.id=p_session FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 IF NOT EXISTS(SELECT FROM photo.projects p WHERE p.tenant_id=p_tenant AND p.id=s.project_id AND p.archived_at IS NULL
  AND (p.visibility<>'restricted' OR member_role IN('owner','admin') OR EXISTS(
   SELECT FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant AND acl.project_id=p.id AND acl.user_id=p_actor))) THEN
  RETURN;
 END IF;
 RETURN QUERY SELECT s.project_id,s.revision,l.document_id FROM (SELECT 1) singleton
 LEFT JOIN photo.shot_lists l ON l.tenant_id=p_tenant AND l.session_id=s.id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v24_shot_snapshot(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v24_shot_snapshot(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v24_shot_snapshot(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
