package v2qualification

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"strings"
)

var reportPublicationABI = []struct {
	Signature string
	Public    bool
	Result    string
}{
	{Signature: "photo.guard_qualification_policy_identity()", Public: false, Result: "trigger"},
	{Signature: "photo.qualification_report_tenant_quiescent(uuid)", Public: true, Result: "boolean"},
	{Signature: "photo.qualification_publication_authorized(uuid,uuid,uuid,text,text)", Public: false, Result: "boolean"},
	{Signature: "photo.guard_qualification_publication()", Public: false, Result: "trigger"},
	{Signature: "photo.reserve_qualification_report(uuid,uuid,uuid,text,text,text,text)", Public: true, Result: "TABLE(artifact uuid,token uuid,writer_state text,object_version text)"},
	{Signature: "photo.check_qualification_report(uuid,uuid,uuid,text,uuid,uuid)", Public: true, Result: "boolean"},
	{Signature: "photo.start_qualification_report_write(uuid,uuid,uuid,text,uuid,uuid)", Public: true, Result: "boolean"},
	{Signature: "photo.close_qualification_report_write(uuid,uuid,uuid,text,uuid,uuid,text)", Public: true, Result: "boolean"},
	{Signature: "photo.complete_qualification_report(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,text)", Public: true, Result: "boolean"},
	{Signature: "photo.claim_qualification_report_cleanup()", Public: true, Result: "TABLE(artifact uuid,tenant uuid,project uuid,token uuid,writer_state text,object_key text,object_version text,sha text,bytes bigint,bucket text)"},
	{Signature: "photo.close_qualification_report_cleanup_writer(uuid,uuid,text)", Public: true, Result: "boolean"},
	{Signature: "photo.finish_qualification_report_cleanup(uuid,uuid,uuid,text)", Public: true, Result: "boolean"},
}

func RequireReportPublicationSchema(ctx context.Context, db *pgxpool.Pool) error {
	if db == nil {
		return ErrUnqualified
	}
	var role, owner string
	var super, login bool
	if err := db.QueryRow(ctx, `SELECT current_user,session.rolsuper,session.rolcanlogin FROM pg_roles session WHERE session.rolname=session_user`).Scan(&role, &super, &login); err != nil {
		return err
	}
	if role != "photo_maintenance" || super || !login {
		return errors.New("maintenance LOGIN required")
	}
	if err := db.QueryRow(ctx, `SELECT rolname FROM pg_roles WHERE rolname='photo_fn_owner' AND NOT rolcanlogin`).Scan(&owner); err != nil {
		return err
	}
	var tableSafe bool
	if err := db.QueryRow(ctx, `SELECT coalesce((SELECT c.relrowsecurity AND c.relforcerowsecurity AND NOT has_table_privilege('photo_maintenance',c.oid,'INSERT,UPDATE,DELETE') AND NOT has_table_privilege('photo_api',c.oid,'INSERT,UPDATE,DELETE') AND NOT has_table_privilege('photo_worker',c.oid,'INSERT,UPDATE,DELETE') AND NOT has_table_privilege('photo_scheduler',c.oid,'INSERT,UPDATE,DELETE') FROM pg_class c WHERE c.oid=to_regclass('photo.qualification_report_publications')),false)`).Scan(&tableSafe); err != nil {
		return err
	}
	if !tableSafe {
		return errors.New("qualification publication table authority invalid")
	}
	for _, item := range reportPublicationABI {
		var valid bool
		var result string
		err := db.QueryRow(ctx, `SELECT coalesce((SELECT p.prosecdef AND p.provolatile='v' AND p.prokind='f' AND owner.rolname='photo_fn_owner' AND p.proconfig=ARRAY['search_path=pg_catalog, photo']::text[] AND l.lanname='plpgsql' AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE') AND has_function_privilege('photo_maintenance',p.oid,'EXECUTE')=$2 AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE') AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE') AND NOT has_function_privilege('photo_scheduler',p.oid,'EXECUTE') AND NOT has_function_privilege('photo_portal',p.oid,'EXECUTE') AND NOT has_function_privilege('photo_gateway',p.oid,'EXECUTE') AND NOT has_function_privilege('photo_auth',p.oid,'EXECUTE') AND NOT has_function_privilege('photo_publisher',p.oid,'EXECUTE') FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner JOIN pg_language l ON l.oid=p.prolang WHERE p.oid=to_regprocedure($1)),false),coalesce(pg_get_function_result(to_regprocedure($1)),'')`, item.Signature, item.Public).Scan(&valid, &result)
		if err != nil {
			return err
		}
		if !valid || strings.ToLower(strings.Join(strings.Fields(result), "")) != strings.ToLower(strings.Join(strings.Fields(item.Result), "")) {
			return errors.New("qualification report publication ABI/authority missing")
		}
	}
	return nil
}

type ReportOperator struct {
	DB      *pgxpool.Pool
	Objects *objectstore.Store
	Bucket  string
	Cleanup ReportPublicationCleanup
}

func NewReportOperator(ctx context.Context, db *pgxpool.Pool, objects *objectstore.Store, bucket string) (ReportOperator, error) {
	if objects == nil || len(bucket) < 3 {
		return ReportOperator{}, ErrUnqualified
	}
	if err := RequireReportPublicationSchema(ctx, db); err != nil {
		return ReportOperator{}, err
	}
	return ReportOperator{DB: db, Objects: objects, Bucket: bucket, Cleanup: ReportPublicationCleanup{DB: db, Objects: objects, Bucket: bucket}}, nil
}
func (o ReportOperator) PublishAndImport(ctx context.Context, s ReportPublicationScope, raw []byte, e Expected) (artifactjson.Ref, uuid.UUID, error) {
	if e.MPSResources != nil {
		if err := RequireMixedQualificationSchema(ctx, o.DB); err != nil {
			return artifactjson.Ref{}, uuid.Nil, err
		}
	}
	body := append([]byte(nil), raw...)
	repository := reportBodyRepository{SQLReportPublicationRepository: SQLReportPublicationRepository{DB: o.DB, Bucket: o.Bucket}, Body: string(body)}
	ref, err := (ReportPublisher{Repository: repository, Objects: o.Objects}).Publish(ctx, s, body, e)
	if err != nil {
		return artifactjson.Ref{}, uuid.Nil, err
	}
	id, err := (Importer{DB: o.DB, Objects: o.Objects, Bucket: o.Bucket}).Import(ctx, s.Tenant, s.Project, ref.ID, ref.SHA256, s.Operator, e)
	return ref, id, err
}

// Mixed parent publication/import does not create or replace any child qualification.
func (o ReportOperator) PublishAndImportMixed(ctx context.Context, s ReportPublicationScope, raw []byte, e MixedExpected) (artifactjson.Ref, uuid.UUID, error) {
	if err := RequireMixedQualificationSchema(ctx, o.DB); err != nil {
		return artifactjson.Ref{}, uuid.Nil, err
	}
	body := append([]byte(nil), raw...)
	repository := reportBodyRepository{SQLReportPublicationRepository: SQLReportPublicationRepository{DB: o.DB, Bucket: o.Bucket}, Body: string(body)}
	ref, err := (ReportPublisher{Repository: repository, Objects: o.Objects}).PublishMixed(ctx, s, body, e)
	if err != nil {
		return artifactjson.Ref{}, uuid.Nil, err
	}
	id, err := (Importer{DB: o.DB, Objects: o.Objects, Bucket: o.Bucket}).ImportMixed(ctx, s.Tenant, s.Project, ref.ID, s.Operator, ref.SHA256, e)
	return ref, id, err
}
