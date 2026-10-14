package interchange

import (
 "context"
 "sort"
 "testing"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type exportPreparationStub struct{purposes []string;fail,wrong bool;staged int}
func(s *exportPreparationStub)PublishExportDocument(_ context.Context,_ CreateIdentity,doc CreateDocument)(v2input.Ref,error){
 s.purposes=append(s.purposes,doc.Purpose)
 if s.fail{return v2input.Ref{},objectstore.ErrUnavailable}
 ref:=doc.Ref;if s.wrong{ref.SHA256=rawSHA([]byte("wrong"))};doc.Body[0]='!';return ref,nil
}
func(s *exportPreparationStub)MaterializeExport(_ context.Context,_ CreateIntent,_ FrozenExportDocuments,task v2input.DraftTask)error{
 s.staged++;if len(s.purposes)!=2||s.purposes[0]!="v30_interchange_export_plan"||s.purposes[1]!="task_params"{return objectstore.ErrIntegrity}
 task.ExecutionProfile[0]='!';return nil
}
func TestExportPreparationPublishesBeforeMaterializationAndOwnsBuffers(t *testing.T){
 _,_,intent,compiled,ref,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,ref,raw);if err!=nil{t.Fatal(err)}
 entries:=compiled.InputRoots();sort.Slice(entries,func(i,j int)bool{return entries[i].Ref.ArtifactID.String()<entries[j].Ref.ArtifactID.String()})
 digest,err:=canonical.Hash(entries);if err!=nil{t.Fatal(err)}
 inventory:=ExportInputInventory{task:compiled.TaskID(),entries:entries,digest:digest}
 resources:=ExportTaskResources{RAMBytes:2<<30,ScratchBytes:2<<30,OutputBytes:1<<30,MaxInputJSONBytes:128<<20,ContractsSHA:rawSHA([]byte("schemas"))}
 stage:=&exportPreparationStub{}
 task,err:=PrepareExportTask(context.Background(),intent,frozen,inventory,resources,stage,stage)
 if err!=nil||stage.staged!=1||task.ExecutionProfile[0]!='{'||compiled.planBody[0]!='{'||compiled.paramsBody[0]!='{'{t.Fatal("preparation/order/ownership failure",err)}
 for _,mode:=range []string{"publication","receipt","resources"}{t.Run(mode,func(t *testing.T){
  stub:=&exportPreparationStub{fail:mode=="publication",wrong:mode=="receipt"};r:=resources;if mode=="resources"{r.ContractsSHA="invalid"}
  got,err:=PrepareExportTask(context.Background(),intent,frozen,inventory,r,stub,stub)
  if err==nil||stub.staged!=0||got.ID!= (v2input.DraftTask{}).ID{t.Fatal("invalid preparation staged")}
  if mode=="resources"&&len(stub.purposes)!=0{t.Fatal("invalid resource pin reached publication")}
 })}
}
