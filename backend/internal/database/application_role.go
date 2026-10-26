package database

import (
 "context"
 "errors"
 "time"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

type RoleQueryer interface { QueryRow(context.Context,string,...any) pgx.Row }

func ApplicationRoleAllowed(role string) bool {
 switch role {case "photo_api","photo_auth","photo_portal","photo_worker","photo_scheduler","photo_publisher","photo_maintenance","photo_gateway","photo_mail","photo_semantic_admission","photo_event_consumer":return true}
 return false
}

// A correct role name does not prove RLS safety if an operator accidentally
// alters its attributes or gives it a privileged SET ROLE membership.
func RequireApplicationRole(parent context.Context, database RoleQueryer, expected string) error {
 if database==nil||!ApplicationRoleAllowed(expected){return errors.New("application database role installation invalid")}
 ctx,cancel:=context.WithTimeout(parent,5*time.Second);defer cancel()
 if err:=ctx.Err();err!=nil{return err}
 var safe bool
 err:=database.QueryRow(ctx,`SELECT current_user=$1 AND NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcreaterole AND NOT r.rolreplication
  AND EXISTS(SELECT FROM pg_roles login WHERE login.rolname=session_user
    AND NOT login.rolsuper AND NOT login.rolbypassrls AND NOT login.rolcreaterole AND NOT login.rolreplication
    AND login.rolname NOT IN('photo_fn_owner','photo_runtime_owner'))
  AND NOT EXISTS(SELECT FROM pg_roles privileged
    WHERE (privileged.rolsuper OR privileged.rolbypassrls OR privileged.rolcreaterole OR privileged.rolreplication
      OR privileged.rolname IN('photo_fn_owner','photo_runtime_owner')
      OR EXISTS(SELECT FROM pg_namespace n WHERE n.nspname='photo' AND n.nspowner=privileged.oid)
      OR EXISTS(SELECT FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
        WHERE n.nspname='photo' AND c.relowner=privileged.oid)
      OR EXISTS(SELECT FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
        WHERE n.nspname='photo' AND p.prosecdef AND p.proowner=privileged.oid))
      AND (pg_has_role(current_user,privileged.oid,'MEMBER') OR pg_has_role(session_user,privileged.oid,'MEMBER')))
  AND NOT has_schema_privilege(current_user,'photo','CREATE')
  AND NOT has_schema_privilege(session_user,'photo','CREATE')
  FROM pg_roles r WHERE r.rolname=current_user`,expected).Scan(&safe)
 if err!=nil||!safe{return errors.New("application database role bypasses isolation")}
 return ctx.Err()
}

// GuardApplicationRole attests each new physical connection after PostgreSQL
// has applied startup SET ROLE and any pre-existing connection initializer.
func GuardApplicationRole(config *pgxpool.Config) {
 expected := config.ConnConfig.RuntimeParams["role"]
 previous := config.AfterConnect
 config.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
  if previous != nil { if err := previous(ctx, connection); err != nil { return err } }
  return RequireApplicationRole(ctx, connection, expected)
 }
}
