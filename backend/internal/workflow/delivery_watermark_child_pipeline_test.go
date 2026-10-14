package workflow

import (
 "context"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

type watermarkResolverNoIO struct{ calls *int }
func(r watermarkResolverNoIO)ResolveWatermarkChild(context.Context,delivery.ExpansionLease,delivery.PreparePlan,delivery.ExportChildIntent)(DeliveryResolvedWatermarkChild,error){
 (*r.calls)++
 return DeliveryResolvedWatermarkChild{},v2input.ErrInvalid
}

func TestWatermarkChildPipelineAndObserverRejectIncompleteLeaseBeforeResolver(t *testing.T){
 plan,parent,_,_,_:=deliveryExportPlanFixture(t,nil)
 page,err:=plan.ExportChildren(parent,0,1);if err!=nil||len(page.Items)!=1{t.Fatal("child fixture",err)}
 calls:=0
 pipeline:=DeliveryWatermarkChildPipeline{DB:&pgxpool.Pool{},Objects:deliveryAssemblyNoIO{},Reader:&deliveryJSONReader{},Bucket:"photos",Resolver:watermarkResolverNoIO{calls:&calls}}
 valid:=delivery.ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Until:time.Now().Add(time.Minute),Tenant:uuid.New(),Run:parent,Workflow:uuid.New(),Package:uuid.New(),ParentTask:plan.TaskID(),Cursor:0,SlotCount:1}
 for _,scenario:=range []struct{name string;change func(*delivery.ExpansionLease)}{
  {"missing_token",func(l *delivery.ExpansionLease){l.Token=uuid.Nil}},
  {"missing_package",func(l *delivery.ExpansionLease){l.Package=uuid.Nil}},
  {"missing_expiry",func(l *delivery.ExpansionLease){l.Until=time.Time{}}},
  {"foreign_parent",func(l *delivery.ExpansionLease){l.ParentTask=uuid.New()}},
  {"invalid_cursor",func(l *delivery.ExpansionLease){l.Cursor=2}},
  {"missing_slots",func(l *delivery.ExpansionLease){l.SlotCount=0}},
 }{
  t.Run(scenario.name,func(t *testing.T){
   lease:=valid;scenario.change(&lease)
   if _,err:=pipeline.ProcessChild(context.Background(),lease,plan,page.Items[0]);err==nil{t.Fatal("invalid lease admitted")}
   if _,err:=pipeline.ObserveChild(context.Background(),lease,plan,page.Items[0]);err==nil{t.Fatal("invalid lease observed")}
   if calls!=0{t.Fatal("resolver reached for invalid lease",calls)}
  })
 }
}

func TestWatermarkChildPipelineRejectsMissingDependencies(t *testing.T){
 var pipeline DeliveryWatermarkChildPipeline
 result,err:=pipeline.ProcessChild(context.Background(),delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{})
 if err==nil||result!=(DeliveryChildAdmission{}){t.Fatal("empty pipeline admitted",result,err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 result,err=pipeline.ProcessChild(ctx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{})
 if err!=context.Canceled||result!=(DeliveryChildAdmission{}){t.Fatal("cancel ignored",result,err)}
}

func TestInitialWatermarkSourceArraysFailClosed(t *testing.T){
 if err:=verifyInitialWatermarkSourceArrays(context.Background(),nil,v2input.Ref{},nil,nil);err==nil{t.Fatal("missing physical reader accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=verifyInitialWatermarkSourceArrays(ctx,nil,v2input.Ref{},nil,nil);err!=context.Canceled{t.Fatal("cancel ignored",err)}
}

func TestWatermarkChildObserverRejectsMissingDependencies(t *testing.T){
 var pipeline DeliveryWatermarkChildPipeline
 progress,err:=pipeline.ObserveChild(context.Background(),delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{})
 if err==nil||progress{t.Fatal("empty observer admitted",progress,err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if progress,err=pipeline.ObserveChild(ctx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{});err!=context.Canceled||progress{t.Fatal("cancel ignored",progress,err)}
}
