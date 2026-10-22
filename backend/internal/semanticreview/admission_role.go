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

// Optional retouch installation must verify its exact transaction entry point.
// A healthy exposure-only role is not evidence that retouch can be committed.
func verifyShownRetouchAdmissionRole(ctx context.Context, db admissionRoleQueryer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil {
		return ErrUnavailable
	}
	var ready bool
	err := db.QueryRow(ctx, `SELECT current_user='photo_semantic_admission' AND EXISTS(
 SELECT 1 FROM pg_proc p WHERE p.oid=to_regprocedure(
 'photo.commit_semantic_shown_retouch_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb)')
 AND p.prosecdef AND p.proowner='photo_fn_owner'::regrole
 AND p.proconfig @> ARRAY['search_path=pg_catalog, photo']::text[]
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM unnest(ARRAY['photo_api','photo_worker','photo_scheduler','photo_portal','photo_gateway']) runtime(role_name)
 WHERE has_function_privilege(runtime.role_name,p.oid,'EXECUTE')))
 AND (SELECT count(*) FROM pg_proc entry JOIN pg_namespace n ON n.oid=entry.pronamespace
 WHERE n.nspname='photo' AND entry.proname='commit_semantic_shown_retouch_apply_review')=1
 AND NOT EXISTS(SELECT 1 FROM unnest($1::text[]) required(name) WHERE
 (SELECT count(*)=1 AND bool_and(helper.prosecdef
 AND helper.proowner='photo_fn_owner'::regrole
 AND helper.proconfig @> ARRAY['search_path=pg_catalog, photo']::text[]
 AND NOT has_function_privilege(current_user,helper.oid,'EXECUTE'))
 FROM pg_proc helper JOIN pg_namespace n ON n.oid=helper.pronamespace
 WHERE n.nspname='photo' AND helper.proname=required.name) IS DISTINCT FROM true)`, []string{
		"lock_semantic_shown_retouch_decision_batch", "semantic_shown_retouch_batch_nodes_bound",
		"create_semantic_shown_retouch_apply_draft_shell", "reserve_semantic_shown_retouch_apply_quota",
		"create_semantic_shown_retouch_apply_reserved_draft", "register_semantic_shown_retouch_apply_inputs", "seal_semantic_shown_retouch_apply_inputs",
		"activate_semantic_shown_retouch_apply_child", "finish_semantic_shown_retouch_apply_review",
	}).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return ErrUnavailable
	}
	return nil
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
