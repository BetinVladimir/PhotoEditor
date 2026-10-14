package provenance

import (
 "context"
 "fmt"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestSQLCRLCheckpointRefusesMissingDatabaseAndCancellation(t *testing.T){
 store:=SQLSigningCRLCheckpoint{TrustBundleSHA:strings.Repeat("a",64)}
 if store.ObserveSigningCRL(context.Background(),strings.Repeat("b",64),"1",time.Now(),strings.Repeat("c",64))==nil{t.Fatal("missing DB accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if store.ObserveSigningCRL(ctx,"","",time.Time{},"")!=context.Canceled{t.Fatal("cancellation lost")}
 if _,err:=NewDurableSigningCRLCheck(nil,nil,time.Now,time.Hour,nil);err==nil{t.Fatal("missing durable store accepted")}
}

func TestIntegrationCRLCheckpointMonotonicityAndDisabledIssuer(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated DB required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 // Provision only inside the rolled-back test transaction, before assuming API.
 trust:=strings.ReplaceAll(uuid.NewString()+uuid.NewString(),"-","")
 issuer:=strings.Repeat("b",64);sha:=strings.Repeat("c",64)
 if _,err=tx.Exec(ctx,`INSERT INTO photo.v31_crl_issuers(trust_bundle_sha,issuer_sha,enabled) VALUES($1,$2,true),($1,$3,false)`,trust,issuer,strings.Repeat("d",64));err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_api`);err!=nil{t.Fatal(err)}
 now:=time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
 observe:=func(iss,number string,at time.Time,pin string,want bool){
  t.Helper();var accepted bool
  if err:=tx.QueryRow(ctx,`SELECT photo.observe_v31_crl_checkpoint($1,$2,$3,$4,$5)`,trust,iss,number,at,pin).Scan(&accepted);err!=nil{t.Fatal(err)}
  if accepted!=want{t.Fatalf("number=%s accepted=%v want=%v",number,accepted,want)}
 }
 var ready bool
 if err=tx.QueryRow(ctx,`SELECT photo.v31_crl_checkpoint_ready($1,$2::text[])`,trust,[]string{issuer}).Scan(&ready);err!=nil||!ready{t.Fatal("enabled issuer not ready",ready,err)}
 if err=tx.QueryRow(ctx,`SELECT photo.v31_crl_checkpoint_ready($1,$2::text[])`,trust,[]string{issuer,issuer}).Scan(&ready);err!=nil||ready{t.Fatal("duplicate issuer ready",ready,err)}
 if err=tx.QueryRow(ctx,`SELECT photo.v31_crl_checkpoint_ready($1,$2::text[])`,trust,[]string{strings.Repeat("d",64)}).Scan(&ready);err!=nil||ready{t.Fatal("disabled issuer ready",ready,err)}
 observe(issuer,"2",now,sha,true)
 observe(issuer,"2",now,sha,true)
 observe(issuer,"1461501637330902918203684832716283019655932542976",now,sha,false)
 observe(issuer,"1",now,sha,false)
 observe(issuer,"2",now,strings.Repeat("e",64),false)
 observe(issuer,"3",now.Add(-time.Second),sha,false)
 observe(issuer,"3",now.Add(time.Second),sha,true)
 observe(strings.Repeat("d",64),"1",now,sha,false)
 var direct bool
 if err=tx.QueryRow(ctx,`SELECT has_table_privilege('photo_api','photo.v31_crl_checkpoints','INSERT,UPDATE,DELETE') OR has_table_privilege('photo_api','photo.v31_crl_issuers','INSERT,UPDATE,DELETE')`).Scan(&direct);err!=nil||direct{t.Fatal("direct API mutation",direct,err)}
}

func TestCRLStartupRefusesMissingStoreAndMismatchedTrust(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 store:=SQLSigningCRLCheckpoint{TrustBundleSHA:strings.Repeat("a",64)}
 if store.CheckSigningCRLInstallation(ctx,"",nil)!=context.Canceled{t.Fatal("startup cancellation lost")}
 for _,trust:=range []string{strings.Repeat("a",64),strings.Repeat("b",64),""}{
  if store.CheckSigningCRLInstallation(context.Background(),trust,[]string{strings.Repeat("c",64)})==nil{t.Fatal("uninstalled SQL startup accepted")}
 }
}

// Committed fixtures are scoped by a fresh random trust pin and removed by the
// administrative test connection. Every competing observation assumes API.
func TestIntegrationCRLCheckpointConcurrentAPIConnections(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated DB required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 trust:=strings.ReplaceAll(uuid.NewString()+uuid.NewString(),"-","")
 issuer:=strings.Repeat("b",64)
 if _,err=db.Exec(ctx,`INSERT INTO photo.v31_crl_issuers(trust_bundle_sha,issuer_sha,enabled) VALUES($1,$2,true)`,trust,issuer);err!=nil{t.Fatal(err)}
 defer func(){
  cleanup,stop:=context.WithTimeout(context.Background(),5*time.Second);defer stop()
  tx,err:=db.Begin(cleanup);if err!=nil{t.Error(err);return};defer tx.Rollback(cleanup)
  if _,err=tx.Exec(cleanup,`DELETE FROM photo.v31_crl_checkpoints WHERE trust_bundle_sha=$1`,trust);err!=nil{t.Error(err);return}
  if _,err=tx.Exec(cleanup,`DELETE FROM photo.v31_crl_issuers WHERE trust_bundle_sha=$1`,trust);err!=nil{t.Error(err);return}
  if err=tx.Commit(cleanup);err!=nil{t.Error(err)}
 }()
 at:=time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
 observe:=func(number int,pin string)(bool,error){
  tx,err:=db.Begin(ctx);if err!=nil{return false,err};defer tx.Rollback(ctx)
  if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_api`);err!=nil{return false,err}
  var accepted bool
  if err=tx.QueryRow(ctx,`SELECT photo.observe_v31_crl_checkpoint($1,$2,$3,$4,$5)`,trust,issuer,fmt.Sprint(number),at,pin).Scan(&accepted);err!=nil{return false,err}
  if err=tx.Commit(ctx);err!=nil{return false,err};return accepted,nil
 }
 results:=make(chan error,32)
 start:=make(chan struct{})
 for number:=1;number<=32;number++{go func(number int){
  <-start
  accepted,err:=observe(number,fmt.Sprintf("%064x",number))
  if err==nil&&number==32&&!accepted{err=fmt.Errorf("maximum checkpoint refused")}
  results<-err
 }(number)}
 close(start)
 for number:=1;number<=32;number++{if err=<-results;err!=nil{t.Error(err)}}
 if t.Failed(){return}
 var number,pin string
 if err=db.QueryRow(ctx,`SELECT crl_number::text,crl_sha FROM photo.v31_crl_checkpoints WHERE trust_bundle_sha=$1 AND issuer_sha=$2`,trust,issuer).Scan(&number,&pin);err!=nil{t.Fatal(err)}
 if number!="32"||pin!=fmt.Sprintf("%064x",32){t.Fatal("concurrent maximum lost",number,pin)}
 if accepted,err:=observe(31,fmt.Sprintf("%064x",31));err!=nil||accepted{t.Fatal("rollback after race",accepted,err)}
 if accepted,err:=observe(32,fmt.Sprintf("%064x",32));err!=nil||!accepted{t.Fatal("replay after race",accepted,err)}
 if accepted,err:=observe(32,strings.Repeat("e",64));err!=nil||accepted{t.Fatal("equivocation after race",accepted,err)}
}
