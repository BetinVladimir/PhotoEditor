package main

import (
 "photoeditor/backend/internal/database"
 "context"
 "errors"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Open only the scheduler reader when deployment publication is disabled.
// No object writer, raster limits, model pin or worker configuration is built.
func buildRelightReceiptReader(ctx context.Context, dsn string, analysis bool) (*pgxpool.Pool,error) {
 if err:=ctx.Err();err!=nil {return nil,err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 config,err:=pgxpool.ParseConfig(dsn)
 if err!=nil {return nil,errors.New("relight receipt scheduler configuration invalid")}
 config.ConnConfig.RuntimeParams["role"]="photo_scheduler"
 database.GuardApplicationRole(config)
 pool,err:=pgxpool.NewWithConfig(ctx,config)
 if err!=nil {return nil,errors.New("relight receipt scheduler unavailable")}
 signature:="photo.read_v28_relight_stored_request_outcome(uuid,uuid,uuid,uuid,uuid,jsonb)"
 if analysis {signature="photo.read_v28_relight_analysis_stored_outcome(uuid,uuid,uuid,uuid,uuid,jsonb)"}
 var role string
 var installed bool
 err=pool.QueryRow(ctx,"SELECT current_user, CASE WHEN to_regprocedure($1) IS NULL THEN false ELSE has_function_privilege(current_user,$1,'EXECUTE') AND to_regprocedure('photo.v32_relight_receipt_authored_config(jsonb)') IS NOT NULL END",signature).Scan(&role,&installed)
 if err!=nil||role!="photo_scheduler"||!installed {pool.Close();return nil,errors.New("relight receipt installation unavailable")}
 return pool,nil
}
