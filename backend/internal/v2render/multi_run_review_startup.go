package v2render

import (
 "context"
 "errors"
 "github.com/jackc/pgx/v5/pgxpool"
)
func ValidateMultiRunReviewInstallation(ctx context.Context,db *pgxpool.Pool)error{
 if db==nil{return errors.New("multi history database unavailable")}
 var installed bool
 err:=db.QueryRow(ctx,`SELECT coalesce((SELECT p.prosecdef AND owner.rolname='photo_fn_owner'
 AND has_function_privilege('photo_api',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
 FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner
 WHERE p.oid=to_regprocedure('photo.read_v29_multi_run_review_scope(uuid,uuid,uuid,uuid)')),false)`).Scan(&installed)
 if err!=nil{return err};if !installed{return errors.New("multi history authority unavailable")};return ctx.Err()
}
