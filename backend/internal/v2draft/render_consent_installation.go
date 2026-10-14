package v2draft

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type renderConsentInstallationReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// RequireRenderConsentDiagnostic refuses incomplete or overprivileged installs.
// It does not execute the diagnostic or authorize any render operation.
func RequireRenderConsentDiagnostic(ctx context.Context, db renderConsentInstallationReader) error {
	if db == nil { return errors.New("render consent diagnostic database unavailable") }
	var valid bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_proc p
 JOIN pg_roles owner ON owner.oid=p.proowner
 WHERE p.oid=to_regprocedure('photo.v26_render_model_consent_revoked(uuid,uuid,uuid,uuid,text,uuid,uuid,text,uuid,uuid)')
 AND p.prosecdef AND p.prorettype='boolean'::regtype
 AND owner.rolname='photo_fn_owner' AND NOT owner.rolsuper AND NOT owner.rolbypassrls
 AND p.proconfig @> ARRAY['search_path=pg_catalog, photo']::text[]
 AND current_user='photo_api' AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
  WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'))`).Scan(&valid)
	if err != nil { return err }
	if !valid { return errors.New("render consent diagnostic installation invalid") }
	return nil
}
