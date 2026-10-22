package run

import (
	"context"
	"errors"
	"photoeditor/backend/internal/artifactjson"
	"time"
)

const authoredParentInstallationSQL = `SELECT current_user='photo_api' AND (SELECT count(*) FROM (VALUES
 ('photo.begin_authored_render_parent(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text)','boolean','67964d5d8fb1a00006ea68de067b343593548d08a696a3214e3a2568337b1c46','uuid, uuid, uuid, uuid, uuid, uuid, uuid, text, text',ARRAY['p_tenant','p_project','p_snapshot','p_actor','p_session','p_parent','p_workflow','p_config_jcs','p_policy_sha']::text[]),
 ('photo.authored_render_parent_accepted(uuid,uuid,uuid,uuid,uuid)','jsonb','5ab976e7ac933a4d2c2c26d708f789c821432648d650894f83f47835690fe36f','uuid, uuid, uuid, uuid, uuid',ARRAY['p_tenant','p_parent','p_project','p_actor','p_session']::text[])) expected(signature,return_type,body_sha,argtypes,argnames)
JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature)
JOIN pg_roles owner ON owner.oid=p.proowner
WHERE owner.rolname='photo_fn_owner' AND NOT owner.rolcanlogin
AND p.prosecdef AND p.provolatile='v' AND NOT p.proleakproof AND p.proparallel='u'
AND p.prokind='f' AND p.prorettype=to_regtype(expected.return_type) AND NOT p.proretset
AND p.pronargs=cardinality(expected.argnames) AND p.pronargdefaults=0
AND p.proallargtypes IS NULL AND p.proargmodes IS NULL
AND p.proargnames IS NOT DISTINCT FROM expected.argnames AND oidvectortypes(p.proargtypes)=expected.argtypes
AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
AND p.proconfig IS NOT DISTINCT FROM ARRAY['search_path=pg_catalog, photo']::text[]
AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')=expected.body_sha
AND (SELECT count(*) FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))))=2
AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
 WHERE x.privilege_type<>'EXECUTE' OR x.grantor<>p.proowner OR x.is_grantable
 OR x.grantee NOT IN(p.proowner,(SELECT oid FROM pg_roles WHERE rolname='photo_api')))
AND has_function_privilege(current_user,p.oid,'EXECUTE'))=2`

// RequireAuthoredParentInstallation gates configured authored admission before API readiness.
// This exact source checkpoint is not an installation or a qualification grant.
func RequireAuthoredParentInstallation(ctx context.Context, db artifactjson.RowQuerier) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil {
		return errors.New("authored parent database required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ready bool
	if err := db.QueryRow(ctx, authoredParentInstallationSQL).Scan(&ready); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !ready {
		return errors.New("authored parent authority unavailable")
	}
	return nil
}
