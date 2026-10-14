package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
func TestIntegrationExportSnapshotReservationPrivilegesAndMissingAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01592 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signatures:=[]string{"photo.attach_v30_export_snapshot(uuid,uuid,uuid,uuid,text,uuid,uuid,text,text)","photo.reserve_v30_export_snapshot(uuid,uuid,uuid,uuid,text,uuid,uuid,text,text,text,text)","photo.complete_v30_export_snapshot(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text,text,text,text)"}
 for _,signature:=range signatures{for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool;if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("unexpected execute grant",role,signature)}
 }}
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 tid,pid,actor,session:=uuid.New(),uuid.New(),uuid.New(),uuid.New();key:="missing-authority"
 snapshot:=uuid.NewSHA1(tid,[]byte("interchange:export-snapshot:"+pid.String()+":"+actor.String()+":"+key));artifact:=uuid.NewSHA1(snapshot,[]byte("interchange:export-snapshot:artifact:1"))
 if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 var outcome string;var actual,token *uuid.UUID;var ready bool
 if err:=tx.QueryRow(ctx,`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_export_snapshot($1,$2,$3,$4,$5,$6,$7,'{}','{}',$8,'photos')`,tid,actor,session,pid,key,snapshot,artifact,rawSHA([]byte("{}"))).Scan(&outcome,&actual,&token,&ready);err!=nil{t.Fatal(err)}
 if outcome!="forbidden"||actual!=nil||token!=nil||ready{t.Fatal("reserved absent authority",outcome)}
 var attached bool
 if err:=tx.QueryRow(ctx,`SELECT photo.attach_v30_export_snapshot($1,$2,$3,$4,$5,$6,$7,$8,$9)`,tid,actor,session,pid,key,snapshot,artifact,rawSHA([]byte("{}")),rawSHA([]byte("{}"))).Scan(&attached);err!=nil{t.Fatal(err)}
 if attached{t.Fatal("attached absent authority")}
 var count int;if err:=tx.QueryRow(ctx,`SELECT (SELECT count(*) FROM photo.v30_export_snapshot_documents WHERE tenant_id=$1)+(SELECT count(*) FROM photo.snapshots WHERE tenant_id=$1)+(SELECT count(*) FROM photo.snapshot_items WHERE tenant_id=$1)`,tid).Scan(&count);err!=nil{t.Fatal(err)};if count!=0{t.Fatal("denied call left reservation")}
}
