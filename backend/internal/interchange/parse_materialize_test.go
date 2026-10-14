package interchange
import("context";"encoding/json";"testing";"github.com/google/uuid";"photoeditor/backend/internal/v2input")
func TestMaterializeTaskOwnsPublishedDigestAndFrozenProfile(t *testing.T){
 tenant,who,request,evidence,limits:=pendingFixture()
 plan,err:=CompileParsePlan(tenant,who,request,"key",evidence,limits);if err!=nil{t.Fatal(err)}
 transport,err:=plan.PublishInputs(context.Background(),&transportPublisher{});if err!=nil{t.Fatal(err)}
 tx:=&proofTx{accepted:true}
 if err:=transport.MaterializeTask(context.Background(),tx,who,tenant,request,"key");err!=nil{t.Fatal(err)}
 if len(tx.args)!=7||tx.args[0]!=tenant||tx.args[1]!=who.UserID||tx.args[2]!=who.SessionID||tx.args[5]!=string(plan.paramsRaw){t.Fatal("materialization changed scope")}
 var draft v2input.DraftTask
 if err:=json.Unmarshal([]byte(tx.args[6].(string)),&draft);err!=nil{t.Fatal(err)}
 expected,err:=transport.DraftTask();if err!=nil{t.Fatal(err)}
 if draft.ID!=expected.ID||draft.InputDigest!=expected.InputDigest||draft.ParamsSHA!=expected.ParamsSHA||string(draft.ExecutionProfile)!=string(expected.ExecutionProfile){t.Fatal("materialization changed frozen task")}
 tx.args=nil
 if transport.MaterializeTask(context.Background(),tx,who,uuid.New(),request,"key")==nil||tx.args!=nil{t.Fatal("foreign tenant queried")}
 changed:=request;changed.ProjectID=uuid.New()
 if transport.MaterializeTask(context.Background(),tx,who,tenant,changed,"key")==nil||tx.args!=nil{t.Fatal("project substitution queried")}
 tx.accepted=false
 if transport.MaterializeTask(context.Background(),tx,who,tenant,request,"key")==nil{t.Fatal("refusal ignored")}
 tx.args=nil;ctx,cancel:=context.WithCancel(context.Background());cancel()
 if transport.MaterializeTask(ctx,tx,who,tenant,request,"key")!=context.Canceled||tx.args!=nil{t.Fatal("cancelled mutation")}
}
