package maintenance

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

// DDL mutations are isolated/rolled back; authored, not executed.
func TestIntegrationResourceInstallationRejectsWrongSetAndOutContracts(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("disposable migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,scenario:=range []struct{name,ddl string}{
 {"installed",""},
 {"scalar_claim",`DROP FUNCTION photo.claim_resource_withdrawal(); CREATE FUNCTION photo.claim_resource_withdrawal() RETURNS record LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$ BEGIN RETURN NULL;END $$; ALTER FUNCTION photo.claim_resource_withdrawal() OWNER TO photo_fn_owner; REVOKE ALL ON FUNCTION photo.claim_resource_withdrawal() FROM PUBLIC; GRANT EXECUTE ON FUNCTION photo.claim_resource_withdrawal() TO photo_maintenance`},
 {"set_finish",`DROP FUNCTION photo.finish_resource_withdrawal(uuid,uuid,text,uuid,uuid); CREATE FUNCTION photo.finish_resource_withdrawal(uuid,uuid,text,uuid,uuid) RETURNS SETOF boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$ BEGIN RETURN NEXT false;END $$; ALTER FUNCTION photo.finish_resource_withdrawal(uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner; REVOKE ALL ON FUNCTION photo.finish_resource_withdrawal(uuid,uuid,text,uuid,uuid) FROM PUBLIC; GRANT EXECUTE ON FUNCTION photo.finish_resource_withdrawal(uuid,uuid,text,uuid,uuid) TO photo_maintenance`},
 {"wrong_claim_column",`DROP FUNCTION photo.claim_resource_withdrawal(); CREATE FUNCTION photo.claim_resource_withdrawal() RETURNS TABLE(job_id uuid,cleanup_token uuid,tenant_id uuid,resource_type text,resource_id text) LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$ BEGIN RETURN;END $$; ALTER FUNCTION photo.claim_resource_withdrawal() OWNER TO photo_fn_owner; REVOKE ALL ON FUNCTION photo.claim_resource_withdrawal() FROM PUBLIC; GRANT EXECUTE ON FUNCTION photo.claim_resource_withdrawal() TO photo_maintenance`},
 {"public_finish",`GRANT EXECUTE ON FUNCTION photo.finish_resource_withdrawal(uuid,uuid,text,uuid,uuid) TO PUBLIC`},
 {"exposed_private_helper",`GRANT EXECUTE ON FUNCTION photo.withdrawal_runs(uuid,text,uuid) TO photo_maintenance`},
 }{t.Run(scenario.name,func(t *testing.T){tx,e:=db.Begin(ctx);if e!=nil{t.Fatal(e)};defer tx.Rollback(context.WithoutCancel(ctx));if scenario.ddl!=""{if _,e=tx.Exec(ctx,scenario.ddl);e!=nil{t.Fatal(e)}};if _,e=tx.Exec(ctx,`SET LOCAL ROLE photo_maintenance`);e!=nil{t.Fatal(e)};e=RequireResourceWithdrawal(ctx,tx);if (e==nil)!=(scenario.name=="installed"){t.Fatal("invalid installation accepted",e)}})}
}
