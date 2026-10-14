package interchange

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
	"time"
)

// Read-only startup check. It never grants roles, applies migrations or changes
// tenant scope, and must use the dedicated scheduler connection pool.
func ValidateSQLImportContinuationInstallation(ctx context.Context, db *pgxpool.Pool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil {
		return v2input.ErrInvalid
	}
	signatures := []string{
		"photo.v30_claim_next_interchange_import()",
		"photo.v30_import_person_mask_qualification_object(uuid,uuid,text,jsonb,uuid,text)",
		"photo.v30_import_stats_qualification_object(uuid,uuid,text,jsonb,uuid,text)",
		"photo.v30_import_upscale_qualification_object(uuid,uuid,text,jsonb,uuid,text)",
		"photo.v30_import_denoise_qualification_object(uuid,uuid,text,jsonb,uuid,text)",
		"photo.v30_interchange_prepared_background_certificate(uuid,uuid,text,uuid,jsonb,uuid,text)",
		"photo.v30_interchange_prepared_warp_geometry(uuid,uuid,text,uuid,jsonb,uuid,text)",
		"photo.v30_interchange_import_sources(uuid,uuid,text)",
		"photo.v30_interchange_metadata_receipts(uuid,uuid,text)",
		"photo.v30_interchange_import_document(uuid,uuid,text,uuid,text,text)",
		"photo.v30_interchange_import_file(uuid,uuid,text,uuid,text)",
		"photo.v30_interchange_import_pixels(uuid,uuid,text,uuid,text,uuid,text,text)",
		"photo.v30_interchange_import_warp(uuid,uuid,text,uuid,text,uuid,text,text)",
		"photo.reserve_v30_interchange_import_document(uuid,uuid,text,uuid,text,text,text)",
		"photo.complete_v30_interchange_import_document(uuid,uuid,text,uuid,text,text,text,uuid,text)",
		"photo.materialize_v30_interchange_import(uuid,uuid,text,text,text,jsonb)",
		"photo.reserve_v2_input_document(uuid,uuid,uuid,text,text,bigint,text)",
		"photo.complete_v2_input_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)",
		"photo.register_v30_interchange_input_batch(uuid,uuid,uuid,integer,jsonb,text)",
		"photo.seal_v30_interchange_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb,text)",
		"photo.v30_import_review_live_decision(uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,text)",
		"photo.activate_v30_interchange_import_review_fenced(uuid,uuid,text,text,text,jsonb,jsonb)",
		"photo.read_v30_interchange_import_activation(uuid,uuid,text,text,text,jsonb)",
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var role string
	var installed bool
	err := db.QueryRow(bounded, `SELECT current_user,
  NOT has_function_privilege(current_user,'photo.v30_next_interchange_import()','EXECUTE')
 AND NOT has_function_privilege(current_user,'photo.v30_import_discovery_uuid(text)','EXECUTE')
 AND NOT has_function_privilege(current_user,'photo.activate_v30_interchange_import(uuid,uuid,text,text,text,jsonb)','EXECUTE')
 AND NOT has_column_privilege(current_user,'photo.v2_workflows','v30_import_retry_after','UPDATE')
 AND (SELECT count(*)=cardinality($1::text[]) AND coalesce(bool_and(
   p.prosecdef AND owner.rolname='photo_fn_owner' AND has_function_privilege(current_user,p.oid,'EXECUTE')
   AND 'search_path=pg_catalog, photo'=ANY(p.proconfig)
   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')),false)
  FROM unnest($1::text[]) signatures(signature)
  JOIN pg_proc p ON p.oid=to_regprocedure(signatures.signature)
  JOIN pg_roles owner ON owner.oid=p.proowner)`, signatures).Scan(&role, &installed)
	if err != nil {
		return err
	}
	if role != "photo_scheduler" || !installed {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}
