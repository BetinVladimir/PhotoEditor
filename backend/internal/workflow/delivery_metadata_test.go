package workflow

import (
 "bytes"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

func TestDeliveryMetadataNodeIdentityRegistryAndInputs(t *testing.T){
 author:="Author"
 metadata,_:=canonical.Marshal(delivery.MetadataDocument{Author:&author})
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},CanvasHash:strings.Repeat("b",64)}
 metadataRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:renderDerivativeBytesSHA(metadata)}
 frozen,pin,err:=delivery.FreezeDeliveryMetadata(binding,metadataRef,metadata,"copyright",nil)
 if err!=nil{t.Fatal(err)}
 raster:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("c",64)}
 encoded,_:=canonical.Marshal(map[string]any{"artifact":raster,"format":"jpeg","width":4,"height":3,"icc_id":"srgb_v1","icc_sha256":strings.Repeat("d",64),"alpha":false,"metadata_policy":"strip"})
 spec:=DeliveryMetadataSpec{ChildRun:uuid.New(),Encoded:v2input.Ref{ArtifactID:uuid.New(),SHA256:renderDerivativeBytesSHA(encoded)},EncodedJCS:encoded,FrozenJCS:frozen,FrozenSHA:pin,Binding:binding,Metadata:metadataRef,MetadataJCS:metadata,Policy:"copyright"}
 op:=Operation{Name:"metadata.write",Version:"1",ResourceClass:"cpu",TimeoutSeconds:180,Backends:[]string{"cpu"}}
 registry:=Registry{"metadata.write":op}
 node,plan,err:=CompileDeliveryMetadata(spec,registry)
 if err!=nil{t.Fatal(err)}
 workflow,_:=v2input.WorkflowID(spec.ChildRun)
 expected,_:=v2input.TaskID(workflow,"metadata.write",0)
 if node.ID!=expected||node.Key!="metadata.write:0"||node.Operation!="metadata.write"||!bytes.Equal(node.Params,plan.ParamsJCS())||len(node.Inputs)!=3||node.Inputs[1].ID!=raster.ArtifactID{t.Fatal("metadata node binding")}
 limits:=DeliveryDerivativeLimits{RAMBytes:32<<30,ScratchBytes:32<<30,OutputBytes:1<<30}
 profile,err:=CompileDeliveryMetadataProfile(node,encoded,4096,limits)
 if err!=nil{t.Fatal("metadata resource profile",err)}
 estimate,err:=ParseResourceEstimate(profile)
 if err!=nil||estimate.VRAMBytes!=0||estimate.PixelCount!=12||estimate.RAMBytes!=(256<<20)+12*64+4096*4||estimate.ScratchBytes!=4096*3+(8<<20){t.Fatal("metadata reservations",estimate,err)}
 for _,small:=range []DeliveryDerivativeLimits{
  {RAMBytes:estimate.RAMBytes-1,ScratchBytes:limits.ScratchBytes,OutputBytes:limits.OutputBytes},
  {RAMBytes:limits.RAMBytes,ScratchBytes:estimate.ScratchBytes-1,OutputBytes:limits.OutputBytes},
  {RAMBytes:limits.RAMBytes,ScratchBytes:limits.ScratchBytes,OutputBytes:estimate.OutputBytes-1}}{
  if _,err=CompileDeliveryMetadataProfile(node,encoded,4096,small);err==nil{t.Fatal("underreserved metadata accepted")}
 }
 assertMetadataStageActivatedReplay(t,spec,registry,limits)
 draft,preparedPlan,err:=PrepareDeliveryMetadataTask(spec,registry,4096,limits)
 closure,closureErr:=plan.InputClosure(spec.ChildRun)
 if err!=nil||closureErr!=nil||draft.ID!=node.ID||draft.InputDigest!=closure.InputDigest||preparedPlan.Params()!=plan.Params(){t.Fatal("prepared metadata task mismatch",err,closureErr)}
 if _,err=CompileDeliveryMetadataProfile(node,encoded,0,limits);err==nil{t.Fatal("missing physical byte evidence accepted")}
 if _,err=CompileDeliveryMetadataProfile(node,encoded,(2<<30)+1,limits);err==nil{t.Fatal("unbounded physical input accepted")}
 node.Params[0]='!';node.Inputs[0].ID=uuid.Nil
 replay,_,err:=CompileDeliveryMetadata(spec,registry)
 if err!=nil||replay.ID!=expected||replay.Params[0]=='!'||replay.Inputs[0].ID==uuid.Nil{t.Fatal("owned replay")}
 for _,invalid:=range []Operation{{}, {Name:"metadata.write",Version:"2",ResourceClass:"cpu",TimeoutSeconds:180,Backends:[]string{"cpu"}}, {Name:"metadata.write",Version:"1",ResourceClass:"cpu",TimeoutSeconds:180,Backends:[]string{"cuda"}}}{
  if _,_,err=CompileDeliveryMetadata(spec,Registry{"metadata.write":invalid});err==nil{t.Fatal("unqualified registry")}
 }
 spec.FrozenSHA=strings.Repeat("0",64)
 if _,_,err=CompileDeliveryMetadata(spec,registry);err==nil{t.Fatal("changed frozen pin")}
}
