package v2qualification

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Typed MPS/parent publication fails before object IO unless the entire new
// authority surface exists with its closed caller matrix. CPU schema1 is separate.
func RequireMixedQualificationSchema(ctx context.Context, db *pgxpool.Pool) error {
	if db == nil {
		return ErrUnqualified
	}
	for _, f := range []struct{ Signature, Result, Caller string }{
		{"photo.mixed_renderer_report_shape(jsonb)", "boolean", ""},
		{"photo.mps_qualification_publication_authorized(uuid,uuid,uuid,text,text)", "boolean", ""},
		{"photo.mixed_renderer_publication_authorized(uuid,uuid,uuid,text,text)", "boolean", ""},
		{"photo.import_v2_mps_qualification(uuid,uuid,uuid,text,uuid,text,text)", "uuid", "photo_maintenance"},
		{"photo.import_mixed_renderer_qualification(uuid,uuid,uuid,text,uuid,text)", "uuid", "photo_maintenance"},
		{"photo.authorize_operator_mixed_children(uuid,uuid,uuid,jsonb,jsonb,text)", "boolean", "photo_maintenance"},
		{"photo.resolve_mixed_renderer_authority(uuid,uuid)", "jsonb", "photo_api"},
	} {
		var ok bool
		if err := db.QueryRow(ctx, `SELECT coalesce((SELECT p.prosecdef AND NOT p.proretset AND p.prokind='f' AND p.prorettype=to_regtype($2) AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql') AND p.proowner='photo_fn_owner'::regrole AND p.proconfig=ARRAY['search_path=pg_catalog, photo']::text[] AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE') AND NOT EXISTS(SELECT FROM unnest(ARRAY['photo_api','photo_scheduler','photo_worker','photo_gateway','photo_maintenance','photo_portal','photo_auth','photo_publisher','photo_semantic_admission']) role WHERE has_function_privilege(role,p.oid,'EXECUTE') IS DISTINCT FROM (role=$3)) FROM pg_proc p WHERE p.oid=to_regprocedure($1)),false)`, f.Signature, f.Result, f.Caller).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrUnqualified
		}
	}
	return nil
}
