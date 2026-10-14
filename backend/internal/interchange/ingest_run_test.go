package interchange
import("context";"encoding/json";"testing";"github.com/google/uuid";"photoeditor/backend/internal/v2input")
func TestImportedIngestRunBindsSnapshotAndRefusesSubstitution(t *testing.T){
 tenant,who,_,_,_:=pendingFixture();job:=uuid.New();plan,err:=CompileImportedIngest(job,[]ImportedImageEvidence{importedImage("source.nef")});if err!=nil{t.Fatal(err)}
 body:=[]byte(`{"schema_version":1}`);parsed:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)};tx:=&proofTx{accepted:true}
 if err:=plan.BeginRun(context.Background(),tx,who,tenant,job,parsed,body);err!=nil{t.Fatal(err)}
 if len(tx.args)!=8||tx.args[0]!=tenant||tx.args[3]!=job||tx.args[4]!=parsed.ArtifactID||tx.args[6]!=string(body){t.Fatal("run scope changed")}
 var items []importedSnapshotItem;if err:=json.Unmarshal([]byte(tx.args[7].(string)),&items);err!=nil{t.Fatal(err)}
 if len(items)!=1||items[0].Ordinal!=0||items[0].Payload.Suffix!=".nef"||items[0].Payload.AssumeProfile!=nil{t.Fatal("snapshot quality options changed")}
 tx.args=nil
 if plan.BeginRun(context.Background(),tx,who,tenant,uuid.New(),parsed,body)==nil||tx.args!=nil{t.Fatal("foreign job reached SQL")}
 if plan.BeginRun(context.Background(),tx,who,tenant,job,parsed,[]byte(`{}`))==nil||tx.args!=nil{t.Fatal("substituted bytes reached SQL")}
 tx.accepted=false;if plan.BeginRun(context.Background(),tx,who,tenant,job,parsed,body)==nil{t.Fatal("SQL refusal ignored")}
}
