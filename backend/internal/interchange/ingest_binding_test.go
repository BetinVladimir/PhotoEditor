package interchange
import("context";"testing";"github.com/google/uuid";"photoeditor/backend/internal/v2input")
func TestImportedBindingRejectsSubstitutionBeforeSQL(t *testing.T){
 tenant,who,_,_,_:=pendingFixture();job:=uuid.New();run:=uuid.NewSHA1(job,[]byte("interchange:canonical-ingest:1"));body:=[]byte(`{"schema_version":1}`);parsed:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)};tx:=&proofTx{}
 if _,err:=BindImportedIngest(context.Background(),tx,who,tenant,job,uuid.New(),parsed,body,0);err==nil||tx.args!=nil{t.Fatal("foreign run queried")}
 if _,err:=BindImportedIngest(context.Background(),tx,who,tenant,job,run,parsed,[]byte(`{}`),0);err==nil||tx.args!=nil{t.Fatal("substituted bytes queried")}
 if _,err:=BindImportedIngest(context.Background(),tx,who,tenant,job,run,parsed,body,5000);err==nil||tx.args!=nil{t.Fatal("invalid ordinal queried")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=BindImportedIngest(ctx,tx,who,tenant,job,run,parsed,body,0);err!=context.Canceled||tx.args!=nil{t.Fatal("cancelled binding queried")}
}
