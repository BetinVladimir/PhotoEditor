package delivery
import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/auth"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestWatermarkRequestBindsPrepareProofAndPin(t *testing.T){
 snapshot,raw,policy,configs:=freezeFixture(t)
 scope,config,evidence:=watermarkPinFixture(t)
 scope.Preset=configs[0].ID;configs[0].Raw=config;configs[0].SHA=scope.PresetSHA;configs[0].Ref.SHA256=scope.PresetSHA
 _,frozenRaw,sha,err:=CompileFrozenInput(snapshot,raw,policy,configs);if err!=nil{t.Fatal(err)}
 frozen:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha};session:=uuid.New()
 pin,err:=CompileWatermarkSourcePin(scope,config,evidence);if err!=nil{t.Fatal(err)}
 pins:=[]PreparedWatermarkPin{{Preset:scope.Preset,PresetSHA:scope.PresetSHA,Pin:pin}}
 _,_,params,err:=CompileWatermarkPrepareProof(scope,session,frozen,frozenRaw,raw,configs,pins);if err!=nil{t.Fatal(err)}
 plan,err:=CompileWatermarkPreparePlan(scope,session,v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(params)},frozen,frozenRaw,raw,configs,pins);if err!=nil{t.Fatal(err)}
 lease:=validExpansionLease();lease.Tenant=scope.Tenant;lease.Run=scope.Parent;lease.Workflow,_=v2input.WorkflowID(scope.Parent);lease.ParentTask=plan.TaskID();lease.SlotCount=plan.preparation.SlotCount
 var proof PrepareProof;if json.Unmarshal(plan.ProofJCS(),&proof)!=nil{t.Fatal("proof fixture")}
 payloadRaw,err:=json.Marshal(expansionPlanPayload{Proof:proof,Params:plan.params,Input:plan.input});if err!=nil{t.Fatal(err)}
 for _,revoke:=range []bool{false,true}{reader:=&watermarkExpansionPins{pin:pin,scope:scope,revoke:revoke};store:=ExpansionLeaseStore{DB:&deliveryReadTx{raw:payloadRaw}};restored,err:=store.LoadWatermarkPlan(context.Background(),lease,scope,reader);if (err==nil)==revoke{t.Fatal("watermark plan pin recheck",err)};if !revoke&&(restored.TaskID()!=plan.TaskID()||reader.calls!=2){t.Fatal("plan restore/recheck")} ;if !revoke{
  before,_:=json.Marshal(plan.watermarkAssemblyRoots);after,_:=json.Marshal(restored.watermarkAssemblyRoots)
  if !bytes.Equal(before,after)||len(restored.watermarkAssemblyRoots)!=3{t.Fatal("restored plan lost watermark assembly roots")}
 }}
 wrongScope:=scope;wrongScope.Project=uuid.New();reader:=&watermarkExpansionPins{pin:pin,scope:scope};if _,err:=(ExpansionLeaseStore{DB:&deliveryReadTx{raw:payloadRaw}}).LoadWatermarkPlan(context.Background(),lease,wrongScope,reader);err==nil||reader.calls!=0{t.Fatal("foreign project read pins")}
 proofScope:=PrepareProofScope{Tenant:scope.Tenant,Project:scope.Project,Run:scope.Parent,Task:plan.TaskID(),Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(params)},Frozen:frozen}
 who:=auth.Identity{UserID:scope.Actor,SessionID:session};tx:=&proofTx{accepted:true}
 if err:=SaveWatermarkPrepareProof(context.Background(),tx,who,proofScope,frozenRaw,raw,configs,pins);err!=nil||tx.calls!=1||len(tx.args)!=10||tx.args[1]!=scope.Actor||tx.args[2]!=session||tx.args[8]!=string(params){t.Fatal("save proof ABI",err)}
 tx.accepted=false;if err:=SaveWatermarkPrepareProof(context.Background(),tx,who,proofScope,frozenRaw,raw,configs,pins);err==nil{t.Fatal("SQL refusal ignored")}
 changedProof:=proofScope;changedProof.Task=uuid.New();before:=tx.calls;if err:=SaveWatermarkPrepareProof(context.Background(),tx,who,changedProof,frozenRaw,raw,configs,pins);err==nil||tx.calls!=before{t.Fatal("foreign prepare task queried")}
 ctx,cancel:=context.WithCancel(context.Background());cancel();if err:=SaveWatermarkPrepareProof(ctx,tx,who,proofScope,frozenRaw,raw,configs,pins);err!=context.Canceled||tx.calls!=before{t.Fatal("cancel ignored",err)}
 page,err:=plan.ExportChildren(scope.Parent,0,1);if err!=nil{t.Fatal(err)};child:=page.Items[0]
 if _,_,_,err:=CompilePrepareProof(scope.Project,session,frozen,frozenRaw,raw,configs);err==nil{t.Fatal("ordinary proof accepted watermark")}
 for _,badPins:=range [][]PreparedWatermarkPin{nil,{pins[0],pins[0]},{{Preset:uuid.New(),PresetSHA:scope.PresetSHA,Pin:pin}}}{if _,_,_,err:=CompileWatermarkPrepareProof(scope,session,frozen,frozenRaw,raw,configs,badPins);err==nil{t.Fatal("incomplete pin inventory")}}
 request,first,err:=plan.WatermarkExportRequest(scope.Parent,child,scope,pin);if err!=nil||request.ConfigJCS!=string(config)||request.Binding!=child.Binding{t.Fatal("request",err)}
 if _,_,err:=plan.ExportRequest(scope.Parent,child);err==nil{t.Fatal("ordinary path accepted watermark")}
 if watermark,err:=plan.ChildUsesWatermark(scope.Parent,child);err!=nil||!watermark{t.Fatal("watermark routing",err)}
 exportID,err:=plan.NewChildExportID(scope.Parent,child)
 if err!=nil||exportID!=uuid.NewSHA1(child.RunID,[]byte("v30:delivery-export:1")){t.Fatal("watermark export identity",err)}
 lease.Cursor=1
 observation:=ExportObservation{ExportID:exportID,SnapshotDigest:child.SnapshotDigest,Binding:child.Binding,PresetSHA:child.Preset.PresetSHA,RunState:"running",ExportState:"queued"}
 observationRaw,err:=json.Marshal(observation);if err!=nil{t.Fatal(err)}
 observed,err:=(ExpansionLeaseStore{DB:&deliveryReadTx{raw:observationRaw}}).ReadExportObservation(context.Background(),lease,plan,child)
 if err!=nil||observed.ExportID!=exportID{t.Fatal("watermark single observation",err)}
 pageRaw,err:=json.Marshal([]ChildObservation{{SlotIndex:0,Export:observation}});if err!=nil{t.Fatal(err)}
 observedPage,err:=(ExpansionLeaseStore{DB:&deliveryReadTx{raw:pageRaw}}).ReadObservationPage(context.Background(),lease,plan,0,1)
 if err!=nil||len(observedPage)!=1||observedPage[0].Export.ExportID!=exportID{t.Fatal("watermark page observation",err)}
 // Exercise the complete frozen watermark matrix through root/shard construction
 // and required-input derivation, rather than only request-level compilation.
 children,err:=plan.ExportChildren(scope.Parent,0,100)
 if err!=nil||children.NextCursor!=plan.preparation.SlotCount{t.Fatal("bounded matrix fixture",err)}
 raster:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest([]byte("watermark encoded raster"))}
 matrix:=make([]ChildObservation,0,len(children.Items))
 for _,item:=range children.Items{
  id,err:=plan.NewChildExportID(scope.Parent,item);if err!=nil{t.Fatal(err)}
  matrix=append(matrix,ChildObservation{SlotIndex:item.SlotIndex,Export:ExportObservation{ExportID:id,SnapshotDigest:item.SnapshotDigest,Binding:item.Binding,PresetSHA:item.Preset.PresetSHA,RunState:"completed",ExportState:"ready",ArtifactReady:true,Artifact:&raster}})
 }
 var shards [][]byte
 _,assemblyRaw,assemblySHA,err:=plan.BuildObservedAssemblyInput(context.Background(),scope.Parent,matrix,func(_ context.Context,_ string,body []byte)(v2input.Ref,error){
  shards=append(shards,append([]byte(nil),body...))
  return v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(body)},nil
 })
 if err!=nil{t.Fatal("watermark assembly construction",err)}
 assemblyRoot:=v2input.Ref{ArtifactID:uuid.New(),SHA256:assemblySHA}
 inputs,err:=plan.AssemblyRequiredInputs(context.Background(),scope.Parent,assemblyRoot,assemblyRaw,shards)
 if err!=nil{t.Fatal("watermark assembly required inputs",err)}
 foundRaster:=false
 for _,entry:=range inputs{if entry.Ref==raster&&entry.Contract=="EncodedExport"{foundRaster=true}}
 if !foundRaster{t.Fatal("watermark raster missing from assembly closure")}
 expectedWatermarkInputs:=[]v2input.Entry{{Ref:evidence.Binding.Source,Contract:"WorkingImage"}}
 for _,array:=range evidence.Arrays{expectedWatermarkInputs=append(expectedWatermarkInputs,v2input.Entry{Ref:array.Ref,Contract:array.Contract})}
 for _,expected:=range expectedWatermarkInputs{
  found:=false;for _,entry:=range inputs{if entry==expected{found=true}}
  if !found{t.Fatal("watermark dependency missing from required assembly closure",expected)}
  for _,mode:=range []string{"missing","wrong contract"}{
   inventory:=make([]v2input.Entry,0,len(inputs))
   for _,entry:=range inputs{
    if entry==expected{if mode=="missing"{continue};entry.Contract="ForeignWatermarkInput"}
    inventory=append(inventory,entry)
   }
   inventoryRaw,err:=json.Marshal(inventory);if err!=nil{t.Fatal(err)}
   current:=lease;current.Cursor=current.SlotCount
   db:=&assemblyInputsDB{root:string(assemblyRaw),shard:string(shards[0]),inventory:inventoryRaw}
   if recovered,err:=(ExpansionLeaseStore{DB:db}).ResolvePublishedAssemblyInputs(context.Background(),current,plan,assemblyRoot);err==nil||recovered!=nil{t.Fatal("incomplete watermark inventory accepted",mode,expected)}
  }

 }

 inventoryRaw,err:=json.Marshal(inputs);if err!=nil{t.Fatal(err)}
 current:=lease;current.Cursor=current.SlotCount
 recovered,err:=(ExpansionLeaseStore{DB:&assemblyInputsDB{root:string(assemblyRaw),shard:string(shards[0]),inventory:inventoryRaw}}).ResolvePublishedAssemblyInputs(context.Background(),current,plan,assemblyRoot)
 if err!=nil||len(recovered)!=len(inputs){t.Fatal("complete watermark assembly inventory refused",err)}
 corruptedShards:=append([][]byte(nil),shards...)
 corruptedShards[0]=append(append([]byte(nil),shards[0]...),byte('!'))
 if _,err:=plan.AssemblyRequiredInputs(context.Background(),scope.Parent,assemblyRoot,assemblyRaw,corruptedShards);err==nil{t.Fatal("corrupt watermark shard accepted")}
 for _,terminal:=range []string{"failed","cancelled"}{
  failedMatrix:=append([]ChildObservation(nil),matrix...)
  failedMatrix[0].Export.RunState=terminal
  failedMatrix[0].Export.ExportState="failed"
  failedMatrix[0].Export.ArtifactReady=false
  failedMatrix[0].Export.Artifact=nil
  witness,err:=plan.FindFailureWitness(context.Background(),scope.Parent,failedMatrix)
  if err!=nil||witness==nil||witness.SlotIndex!=0||witness.ChildRun!=children.Items[0].RunID||witness.ExportID!=matrix[0].Export.ExportID{t.Fatal("watermark terminal witness",terminal,witness,err)}
  publications:=0
  if _,_,_,err:=plan.BuildObservedAssemblyInput(context.Background(),scope.Parent,failedMatrix,func(context.Context,string,[]byte)(v2input.Ref,error){publications++;return v2input.Ref{},nil});err==nil||publications!=0{t.Fatal("failed watermark matrix published assembly",terminal,err,publications)}
  substituted:=append([]ChildObservation(nil),failedMatrix...)
  substituted[0].Export.ExportID=uuid.New()
  if witness,err:=plan.FindFailureWitness(context.Background(),scope.Parent,substituted);err==nil||witness!=nil{t.Fatal("foreign failure witness accepted")}
 }
 changedIdentity:=child;changedIdentity.RunID=uuid.New()
 if _,err:=plan.NewChildExportID(scope.Parent,changedIdentity);err==nil{t.Fatal("foreign watermark child identity accepted")}

 first[0]='!';_,again,err:=plan.WatermarkExportRequest(scope.Parent,child,scope,pin);if err!=nil||bytes.Equal(first,again)||again[0]!='{'{t.Fatal("borrowed request",err)}
 for _,mode:=range []string{"project","actor","tenant","parent","preset","child","source"}{
  changedScope:=scope;changedChild:=child
  switch mode{case "project":changedScope.Project=uuid.New();case "actor":changedScope.Actor=uuid.New();case "tenant":changedScope.Tenant=uuid.New();case "parent":changedScope.Parent=uuid.New();case "preset":changedScope.Preset=uuid.New();case "child":changedChild.RunID=uuid.New();case "source":changedChild.Binding.Source.ArtifactID=uuid.New()}
  if _,_,err:=plan.WatermarkExportRequest(scope.Parent,changedChild,changedScope,pin);err==nil{t.Fatal("substitution",mode)}
 }
 if _,_,err:=plan.WatermarkExportRequest(scope.Parent,child,scope,FrozenWatermarkSource{});err==nil{t.Fatal("missing pin")}
}
