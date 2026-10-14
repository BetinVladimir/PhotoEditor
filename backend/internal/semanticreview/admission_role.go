package semanticreview

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// VerifyExposureAdmissionRole checks the active role and actual ACLs without
// changing them. Login provisioning must additionally prevent RESET ROLE or SET
// ROLE into a privileged contour; this check does not attest login credentials.
func VerifyExposureAdmissionRole(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return ErrUnavailable
	}
	return verifyExposureAdmissionRole(ctx, pool)
}

type admissionRoleQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func verifyExposureAdmissionRole(ctx context.Context, db admissionRoleQueryer) error {
	var ready bool
	err := db.QueryRow(ctx, `SELECT current_user='photo_semantic_admission'
 AND EXISTS(SELECT 1 FROM pg_class WHERE oid='photo.idempotency_records'::regclass AND relrowsecurity AND relforcerowsecurity)
 AND row_security_active('photo.idempotency_records'::regclass)
 AND EXISTS(SELECT 1 FROM pg_roles WHERE rolname=current_user AND NOT (rolsuper OR rolcanlogin OR rolinherit OR rolbypassrls OR rolcreaterole OR rolcreatedb OR rolreplication))
 AND NOT EXISTS(SELECT 1 FROM pg_auth_members WHERE member=current_user::regrole)
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='photo' AND c.relkind IN ('r','p') AND
 (has_table_privilege(current_user,c.oid,'DELETE,TRUNCATE,TRIGGER,REFERENCES')
 OR (c.oid<>'photo.idempotency_records'::regclass AND
 (has_any_column_privilege(current_user,c.oid,'INSERT') OR has_any_column_privilege(current_user,c.oid,'UPDATE')))))
 AND NOT has_column_privilege(current_user,'photo.idempotency_records','request_hash','UPDATE')
 AND has_column_privilege(current_user,'photo.idempotency_records','response_body','UPDATE')
 AND has_column_privilege(current_user,'photo.idempotency_records','tenant_id','INSERT')
 AND NOT EXISTS(SELECT 1 FROM unnest($1::text[]) required(name) WHERE
 (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='photo' AND p.proname=required.name AND p.prosecdef AND p.proowner='photo_fn_owner'::regrole
 AND has_function_privilege(current_user,p.oid,'EXECUTE'))<>1)`, []string{
		"semantic_review_identity", "semantic_approved_protection_pin", "semantic_approved_protection_object", "semantic_approved_protection_origin",
		"semantic_approved_protection_binary", "semantic_approved_image_object", "semantic_approved_image_binary", "semantic_approved_candidate_child",
		"semantic_approved_mask_binary", "create_semantic_apply_draft_shell", "register_semantic_apply_inputs", "seal_semantic_apply_inputs",
		"finish_semantic_apply_review", "lock_semantic_apply_resources",
	}).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return ErrUnavailable
	}
	return nil
}
