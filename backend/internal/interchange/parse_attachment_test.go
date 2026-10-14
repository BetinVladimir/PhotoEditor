package interchange
import("context";"testing";"github.com/google/uuid")
func TestAttachSnapshotBindsRootAndRejectsForeignPlan(t *testing.T){
 tenant,who,request,evidence,limits:=pendingFixture()
 plan,err:=CompileParsePlan(tenant,who,request,"key",evidence,limits);if err!=nil{t.Fatal(err)}
 root,err:=plan.CompileSnapshot();if err!=nil{t.Fatal(err)}
 tx:=&proofTx{accepted:true}
 if err:=plan.AttachSnapshot(context.Background(),tx,who,tenant,request,"key");err!=nil{t.Fatal(err)}
 if len(tx.args)!=7||tx.args[0]!=tenant||tx.args[1]!=who.UserID||tx.args[2]!=who.SessionID||tx.args[5]!=root.Ref.ArtifactID||tx.args[6]!=string(root.Body){t.Fatal("attachment scope or bytes changed")}
 tx.args=nil
 if plan.AttachSnapshot(context.Background(),tx,who,uuid.New(),request,"key")==nil||tx.args!=nil{t.Fatal("foreign tenant reached SQL")}
 tx.accepted=false
 if plan.AttachSnapshot(context.Background(),tx,who,tenant,request,"key")==nil{t.Fatal("refusal ignored")}
 tx.args=nil;ctx,cancel:=context.WithCancel(context.Background());cancel()
 if plan.AttachSnapshot(ctx,tx,who,tenant,request,"key")!=context.Canceled||tx.args!=nil{t.Fatal("cancelled call reached SQL")}
}
