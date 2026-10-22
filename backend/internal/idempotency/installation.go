package idempotency

import (
 "context"
 "errors"
 "time"

 "github.com/jackc/pgx/v5"
)

type InstallationReader interface { QueryRow(context.Context,string,...any) pgx.Row }

// RequireParentBinding verifies the installed column and effective runtime
// privileges. A current binary must refuse startup rather than partially admit
// requests against the old receipt schema. This is not migration execution.
func RequireParentBinding(ctx context.Context, db InstallationReader) error {
 if err:=ctx.Err();err!=nil{return err}
 if db==nil{return errors.New("idempotency installation unavailable")}
 bounded,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 var ready bool
 err:=db.QueryRow(bounded,`SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_attribute a
 WHERE a.attrelid='photo.idempotency_records'::regclass AND a.attname='parent_binding'
 AND a.atttypid='text'::regtype AND NOT a.attisdropped)
 AND has_column_privilege(current_user,'photo.idempotency_records','parent_binding','SELECT')
 AND has_column_privilege(current_user,'photo.idempotency_records','parent_binding','UPDATE')`).Scan(&ready)
 if err!=nil{return err}
 if !ready{return errors.New("idempotency parent binding migration or runtime privileges missing")}
 return bounded.Err()
}
