package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
)

func TestDependencyPackageReturnsOwnedCompleteInventory(t *testing.T){
 origin,run,child,childRun,tenant,project:=uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New()
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 original:=[]NegativeDependencyTask{{TaskID:origin,RunID:run,Parents:[]uuid.UUID{}},{TaskID:child,RunID:childRun,Parents:[]uuid.UUID{origin}}}
 raw,err:=canonical.Marshal(original);if err!=nil{t.Fatal(err)}
 tx:=&negativeInventoryTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:tenant}},raw:raw}
 db:=negativeInventoryDB{tx}
 plan,inventory,err:=ReadNegativeDependencyPackage(context.Background(),db,who,tenant,project,origin,run,"base",10,10)
 if err!=nil||len(inventory)!=2||len(plan.AffectedTasks)!=1||plan.AffectedTasks[0]!=child||tx.commits!=1{t.Fatal("complete package missing",err)}
 inventory[1].Parents[0]=uuid.New();inventory[0].TaskID=uuid.New();plan.AffectedTasks[0]=uuid.New()
 again,againInventory,err:=ReadNegativeDependencyPackage(context.Background(),db,who,tenant,project,origin,run,"base",10,10)
 if err!=nil||again.AffectedTasks[0]!=child||againInventory[1].Parents[0]!=origin||againInventory[0].TaskID!=origin{t.Fatal("caller mutation reached catalogue",err)}
 legacy,err:=ReadNegativeDependencyPlan(context.Background(),db,who,tenant,project,origin,run,"base",10,10)
 if err!=nil{t.Fatal(err)}
 a,err:=canonical.Hash(again);if err!=nil{t.Fatal(err)};b,err:=canonical.Hash(legacy);if err!=nil||a!=b{t.Fatal("legacy reader changed plan",err)}
}

func TestReplacementLoaderMissingDependenciesAndCancellation(t *testing.T){
 db:=&negativeDraftStoreDB{}
 if _,err:=LoadNegativeReplacementInputs(context.Background(),db,nil,"photos",auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{});err==nil||db.begins!=0{t.Fatal("missing inputs reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=LoadNegativeReplacementInputs(ctx,db,nil,"photos",auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{});err!=context.Canceled{t.Fatal("cancellation",err)}
}
