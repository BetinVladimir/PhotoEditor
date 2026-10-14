package interchange

import (
 "context"
 "encoding/json"
 "sort"
 "testing"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestCompileExportTaskPinsCPUProfileAndFrozenInputs(t *testing.T){
 _,_,intent,compiled,ref,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,ref,raw);if err!=nil{t.Fatal(err)}
 entries:=compiled.InputRoots();sort.Slice(entries,func(i,j int)bool{return entries[i].Ref.ArtifactID.String()<entries[j].Ref.ArtifactID.String()})
 digest,err:=canonical.Hash(entries);if err!=nil{t.Fatal(err)}
 inventory:=ExportInputInventory{task:compiled.TaskID(),entries:entries,digest:digest}
 resources:=ExportTaskResources{RAMBytes:2<<30,ScratchBytes:2<<30,OutputBytes:1<<30,MaxInputJSONBytes:128<<20,ContractsSHA:rawSHA([]byte("schemas"))}
 task,err:=CompileExportTask(context.Background(),intent,frozen,inventory,resources);if err!=nil{t.Fatal(err)}
 if task.ID!=intent.Identity.Task||task.ParamsArtifactID!=intent.Identity.Params||task.ParamsSHA!=compiled.paramsRef.SHA256||task.InputDigest!=digest||task.Operation!="v2.interchange.export"||task.ResourceClass!="cpu"{t.Fatal("task binding mismatch")}
 var profile struct{Backend string `json:"backend"`;Renderer string `json:"renderer_version"`;ContractsSHA string `json:"contracts_sha"`;MaxInputJSON int64 `json:"max_input_json_bytes"`;Estimate map[string]int64 `json:"resource_estimate"`}
 if err:=json.Unmarshal(task.ExecutionProfile,&profile);err!=nil{t.Fatal(err)}
 if profile.ContractsSHA!=resources.ContractsSHA||profile.MaxInputJSON!=resources.MaxInputJSONBytes||profile.Backend!="cpu"||profile.Renderer!="v2.2"||profile.Estimate["vram_bytes"]!=0||profile.Estimate["ram_bytes"]!=resources.RAMBytes||profile.Estimate["output_bytes"]!=resources.OutputBytes{t.Fatal("invalid CPU estimate")}
 task.ExecutionProfile[0]='!'
 again,err:=CompileExportTask(context.Background(),intent,frozen,inventory,resources);if err!=nil||again.ExecutionProfile[0]!='{'{t.Fatal("profile alias",err)}
 for _,mode:=range []string{"ram","scratch","output","json","unsafe","inventory","roots","order","schemas"}{t.Run(mode,func(t *testing.T){
  r:=resources;i:=inventory;i.entries=append([]v2input.Entry(nil),entries...)
  switch mode{case "schemas":r.ContractsSHA="invalid";case "ram":r.RAMBytes=1;case "scratch":r.ScratchBytes=1;case "output":r.OutputBytes=1;case "json":r.MaxInputJSONBytes=129<<20;case "unsafe":r.OutputBytes=9007199254740992;case "inventory":i.digest="changed";case "roots":i.entries=i.entries[1:];i.digest,_=canonical.Hash(i.entries);case "order":i.entries[0],i.entries[1]=i.entries[1],i.entries[0];i.digest,_=canonical.Hash(i.entries)}
  if _,err:=CompileExportTask(context.Background(),intent,frozen,i,r);err==nil{t.Fatal("invalid task accepted")}
 })}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=CompileExportTask(ctx,intent,frozen,inventory,resources);err!=context.Canceled{t.Fatal("cancellation ignored",err)}
}
