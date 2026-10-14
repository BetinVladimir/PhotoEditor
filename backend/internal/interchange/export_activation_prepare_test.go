package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
)
type exportResourceQualifierStub struct{resources ExportTaskResources;err error;calls int;mutate bool}
func(s *exportResourceQualifierStub)QualifyExportResources(_ context.Context,intent CreateIntent,frozen FrozenExportDocuments,inventory ExportInputInventory)(ExportTaskResources,error){
 s.calls++
 if s.mutate{intent.RequestJCS[0]='!';*intent.Request.SnapshotID=intent.Identity.Run;frozen.body[0]='!';inventory.entries[0].Contract="substituted"}
 return s.resources,s.err
}
func TestExportActivationPreparationRequiresFreshClosureAndQualifiedExactTask(t *testing.T){
 for _,mode:=range []string{"valid","mutation","missing_resources","missing_resolver","refused","changed_resources","changed_digest","cancelled"}{t.Run(mode,func(t *testing.T){
  _,_,intent,frozen,resolver:=exportExecutionFixture(t)
  _,compiled,err:=DecodeFrozenExportDocuments(context.Background(),intent,frozen.Body());if err!=nil{t.Fatal(err)}
  inventory,err:=ResolveExportInputInventory(context.Background(),compiled,resolver);if err!=nil{t.Fatal(err)}
  resources:=ExportTaskResources{RAMBytes:2<<30,ScratchBytes:2<<30,OutputBytes:1<<30,MaxInputJSONBytes:128<<20,ContractsSHA:rawSHA([]byte("schemas"))}
  task,err:=CompileExportTask(context.Background(),intent,frozen,inventory,resources);if err!=nil{t.Fatal(err)}
  cause:=errors.New("qualification unavailable");qualifier:=&exportResourceQualifierStub{resources:resources};p:=ExportActivationPreparation{Resolver:resolver,Resources:qualifier}
  ctx:=context.Background()
  switch mode{case "mutation":qualifier.mutate=true;case "missing_resources":p.Resources=nil;case "missing_resolver":p.Resolver=nil;case "refused":qualifier.err=cause;case "changed_resources":qualifier.resources.RAMBytes++;case "changed_digest":task.InputDigest=rawSHA([]byte("different"));case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  receipt,err:=p.Prepare(ctx,intent,frozen,task)
  if mode=="valid"||mode=="mutation"{if err!=nil||receipt.task.ID!=intent.Identity.Task||receipt.inventory.digest!=inventory.digest||string(receipt.frozen.Body())!=string(frozen.Body()){t.Fatal("qualified preparation failed/aliased",err)}}else if err==nil||receipt.task.ID!=uuid.Nil{t.Fatal("failed preparation exposed receipt",err)}
  if mode=="refused"&&!errors.Is(err,cause){t.Fatal("qualification error lost")}
  if (mode=="missing_resources"||mode=="missing_resolver"||mode=="cancelled")&&qualifier.calls!=0{t.Fatal("invalid preparation qualified resources")}
 })}
}
