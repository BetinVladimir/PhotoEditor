package run

import (
	"context"
	"errors"
	"photoeditor/backend/internal/artifactjson"
	"time"
)

const editorIdentityInstallationSQL = `SELECT current_user='photo_api'
 AND (SELECT count(*) FROM (VALUES
 ('photo.editor_effect_origin_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,bigint,uuid,uuid)','sql','f8d944a75a31a162bdde127debc9d3b3814fc9e10039262300f02277b72c6c79'),
 ('photo.lock_editor_effect_identity(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,bigint,uuid,uuid)','plpgsql','0862401fb8355863cb9dcf47a5f4faf64dd8538bee0c2765b039bcfbd6a01478')) expected(signature,language,body_sha)
 JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature)
 JOIN pg_roles owner ON owner.oid=p.proowner
 WHERE owner.rolname='photo_fn_owner' AND NOT owner.rolcanlogin
 AND p.prosecdef AND p.provolatile='v' AND NOT p.proleakproof AND p.proparallel='u'
 AND p.prokind='f' AND p.prorettype='boolean'::regtype AND NOT p.proretset
 AND NOT (p.pronargs<>12 OR p.pronargdefaults<>0 OR p.proallargtypes IS NOT NULL OR p.proargmodes IS NOT NULL OR p.proargnames IS DISTINCT FROM ARRAY['p_tenant','p_project','p_version','p_source','p_source_sha','p_recipe','p_recipe_sha','p_frozen','p_draft','p_revision','p_actor','p_session']::text[] OR oidvectortypes(p.proargtypes)<>'uuid, uuid, uuid, uuid, text, uuid, text, uuid, uuid, bigint, uuid, uuid')
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname=expected.language)
 AND p.proconfig IS NOT DISTINCT FROM ARRAY['search_path=pg_catalog, photo']::text[]
 AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')=expected.body_sha
 AND (SELECT count(*) FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))))=2
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
  WHERE x.privilege_type<>'EXECUTE' OR x.grantor<>p.proowner
   OR x.grantee NOT IN(p.proowner,(SELECT oid FROM pg_roles WHERE rolname='photo_api'))
   OR x.is_grantable)
 AND has_function_privilege(current_user,p.oid,'EXECUTE'))=2
 AND NOT EXISTS(SELECT FROM (VALUES('photo_api'),('photo_portal')) caller_role(name)
 CROSS JOIN (VALUES('photo.users'),('photo.sessions')) identity_table(name)
 WHERE has_table_privilege(caller_role.name,identity_table.name,'SELECT') OR has_table_privilege(caller_role.name,identity_table.name,'UPDATE')
 OR has_any_column_privilege(caller_role.name,identity_table.name,'SELECT') OR has_any_column_privilege(caller_role.name,identity_table.name,'UPDATE'))
 AND has_table_privilege('photo_fn_owner','photo.users','SELECT')
 AND has_table_privilege('photo_fn_owner','photo.users','UPDATE')
 AND has_table_privilege('photo_fn_owner','photo.sessions','SELECT')
 AND has_table_privilege('photo_fn_owner','photo.sessions','UPDATE')`

// RequireEditorIdentityInstallation runs on the API pool before readiness.
// It refuses missing/drifted authority and accidental global identity grants.
func RequireEditorIdentityInstallation(ctx context.Context, db artifactjson.RowQuerier) error {
	if db == nil {
		return errors.New("Editor identity lock database required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ready bool
	if err := db.QueryRow(ctx, editorIdentityInstallationSQL).Scan(&ready); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !ready {
		return errors.New("Editor identity lock authority unavailable")
	}
	return nil
}
