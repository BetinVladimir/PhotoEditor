package interchange
import("context";"encoding/json";"testing";"github.com/jackc/pgx/v5/pgxpool";"photoeditor/backend/internal/artifactjson";"photoeditor/backend/internal/v2input")
type parseUncalledObjects struct{artifactjson.Objects}
func TestParsePrepublicationDigestMatchesPublishedInventory(t *testing.T){
 tenant,who,request,evidence,limits:=pendingFixture()
 plan,err:=CompileParsePlan(tenant,who,request,"key",evidence,limits);if err!=nil{t.Fatal(err)}
 tx:=&proofTx{accepted:true}
 if err:=plan.MaterializeParseTask(context.Background(),tx,who,tenant,request,"key");err!=nil{t.Fatal(err)}
 var before v2input.DraftTask
 if err:=json.Unmarshal([]byte(tx.args[6].(string)),&before);err!=nil{t.Fatal(err)}
 transport,err:=plan.PublishInputs(context.Background(),&transportPublisher{});if err!=nil{t.Fatal(err)}
 after,err:=transport.DraftTask();if err!=nil{t.Fatal(err)}
 if before.InputDigest!=after.InputDigest||before.ID!=after.ID||before.ParamsSHA!=after.ParamsSHA||string(before.ExecutionProfile)!=string(after.ExecutionProfile){t.Fatal("prepublication task differs from published inventory")}
 // Returned direct input slices must not mutate the plan used on retry.
 entries:=plan.sortedInputs();entries[0].Contract="substituted"
 if err:=plan.MaterializeParseTask(context.Background(),tx,who,tenant,request,"key");err!=nil{t.Fatal(err)}
 var retry v2input.DraftTask
 if err:=json.Unmarshal([]byte(tx.args[6].(string)),&retry);err!=nil{t.Fatal(err)}
 if retry.InputDigest!=before.InputDigest{t.Fatal("caller mutation changed retry")}
}
func TestPrepareParseRejectsUnavailableDependenciesAndCancellation(t *testing.T){
 tenant,who,request,_,_:=pendingFixture()
 if _,err:=(Service{}).PrepareParse(context.Background(),who,tenant,request,"key",ParseDependencies{});err==nil{t.Fatal("missing production dependencies accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(Service{}).PrepareParse(ctx,who,tenant,request,"key",ParseDependencies{});err!=context.Canceled{t.Fatal("cancellation ignored",err)}
}
func TestPrepareParseRejectsSharedAPIAndSchedulerPool(t *testing.T){
 tenant,who,request,_,_:=pendingFixture()
 shared:=&pgxpool.Pool{}
 _,err:=(Service{DB:shared}).PrepareParse(context.Background(),who,tenant,request,"key",ParseDependencies{
  Scheduler:shared,Bucket:"archive-bucket",Objects:parseUncalledObjects{},Documents:artifactjson.Publisher{},
 })
 if err==nil{t.Fatal("shared API and scheduler pool accepted")}
}
