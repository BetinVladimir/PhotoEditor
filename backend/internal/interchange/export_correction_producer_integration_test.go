package interchange

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/canonical"
)

func TestIntegrationExportCorrectionProducerEvidenceIsolation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signature:="photo.v30_export_correction_producer_evidence(uuid,uuid,uuid,uuid,uuid,text,text)"
 var owner string;var definer,public bool
 if err:=db.QueryRow(ctx,`SELECT pg_get_userbyid(p.proowner),p.prosecdef,
  EXISTS(SELECT FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
  FROM pg_proc p WHERE p.oid=$1::regprocedure`,signature).Scan(&owner,&definer,&public);err!=nil{t.Fatal(err)}
 if owner!="photo_fn_owner"||!definer||public{t.Fatal("incorrect evidence security")}
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2::regprocedure,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatalf("unexpected evidence privilege for %s",role)}
 }
 raw,err:=canonical.Marshal(map[string]any{"schema_version":1,"snapshot_digest":rawSHA([]byte("snapshot")),"items":[]any{map[string]any{"ordinal":2,"binding":map[string]any{"asset_version_id":uuid.New(),"source":map[string]any{"artifact_id":uuid.New(),"sha256":rawSHA([]byte("source"))},"canvas_hash":rawSHA([]byte("canvas"))}}}});if err!=nil{t.Fatal(err)}
 tenantID:=uuid.New()
 for _,mode:=range []string{"missing","foreign_tenant","hash","malformed"}{t.Run(mode,func(t *testing.T){
  tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
  scope:=tenantID;if mode=="foreign_tenant"{scope=uuid.New()}
  if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,scope.String());err!=nil{t.Fatal(err)}
  if _,err:=tx.Exec(ctx,"SET LOCAL ROLE photo_api");err!=nil{t.Fatal(err)}
  body:=raw;digest:=rawSHA(body)
  if mode=="hash"{digest=rawSHA([]byte("foreign"))};if mode=="malformed"{body=[]byte("{");digest=rawSHA(body)}
  var result []byte
  if err:=tx.QueryRow(ctx,`SELECT photo.v30_export_correction_producer_evidence($1,$2,$3,$4,$5,$6,$7)`,tenantID,uuid.New(),uuid.New(),uuid.New(),uuid.New(),digest,string(body)).Scan(&result);err!=nil{t.Fatal(err)}
  if len(result)!=0{t.Fatal("missing/foreign authority returned producer evidence")}
 })}
}
