package workflow
import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)
// Installation evidence only; does not claim positive admission/Apply execution.
func TestIntegrationMultiReplacementStartupCurrentInstallation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 config,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)}
 config.ConnConfig.RuntimeParams["role"]="photo_scheduler"
 pool,err:=pgxpool.NewWithConfig(ctx,config);if err!=nil{t.Fatal(err)};defer pool.Close()
 if err=verifyMultiReplacementInstallation(ctx,pool);err!=nil{t.Fatal(err)}
 tx,err:=pool.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
 // The API has the complete atomic entrypoint, never its incomplete internals.
 var complete,retained,version bool
 if err=tx.QueryRow(ctx,`SELECT
 has_function_privilege('photo_api','photo.apply_v29_multi_complete(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb,jsonb)','EXECUTE'),
 has_function_privilege('photo_api','photo.apply_v29_multi_retained(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb)','EXECUTE'),
 has_function_privilege('photo_api','photo.apply_v29_multi_version(uuid,uuid,text,uuid,uuid,uuid,jsonb,text)','EXECUTE')`).Scan(&complete,&retained,&version);err!=nil{t.Fatal(err)}
 if !complete||retained||version{t.Fatal("Apply bypass or missing API entrypoint")}
}
