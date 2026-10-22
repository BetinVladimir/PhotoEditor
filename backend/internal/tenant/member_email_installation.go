package tenant

import (
	"context"
	"errors"
	"photoeditor/backend/internal/artifactjson"
	"time"
)

// RequireMemberEmailInstallation refuses missing or overprivileged member projections at API startup.
func RequireMemberEmailInstallation(ctx context.Context, db artifactjson.RowQuerier) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil {
		return errors.New("tenant member projection database required")
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ready bool
	if err := db.QueryRow(bounded, `SELECT current_user='photo_api' AND NOT has_any_column_privilege('photo_api','photo.users','SELECT') AND count(*)=1 AND bool_and(
 p.proowner='photo_fn_owner'::regrole AND p.prosecdef AND NOT p.proretset AND p.prokind='f'
 AND p.provolatile='v' AND NOT p.proleakproof AND p.proparallel='u' AND l.lanname='plpgsql'
 AND p.prorettype='text'::regtype AND p.pronargs=4
 AND p.proargtypes='2950 2950 2950 2950'::oidvector
 AND p.proargnames=ARRAY['p_actor','p_session','p_tenant','p_target']::text[]
 AND p.proconfig=ARRAY['search_path=pg_catalog, photo']::text[]
 AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')='bd1683d847db13f03eba69cb85e4f582a2c197e6e47aa3bc81991b7a48119214'
 AND has_function_privilege('photo_api',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE a.privilege_type='EXECUTE' AND (a.grantee NOT IN(p.proowner,'photo_api'::regrole) OR a.is_grantable)))
 FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang
 WHERE p.oid=to_regprocedure('photo.tenant_member_email(uuid,uuid,uuid,uuid)')`).Scan(&ready); err != nil {
		return err
	}
	if err := bounded.Err(); err != nil {
		return err
	}
	if !ready {
		return errors.New("tenant member projection installation unavailable")
	}
	return nil
}
