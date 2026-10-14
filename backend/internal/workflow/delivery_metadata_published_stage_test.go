package workflow

import (
 "context"
 "fmt"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
func TestPublishedMetadataStageCompositionAndScope(t *testing.T){
 author:="Artist";raw,err:=canonical.Marshal(delivery.MetadataDocument{Author:&author});if err!=nil{t.Fatal(err)}
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},CanvasHash:strings.Repeat("b",64)}
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:renderDerivativeBytesSHA(raw)}
 scope:=delivery.MetadataSourcePublicationScope{Tenant:uuid.New(),Project:uuid.New(),Actor:uuid.New(),Session:uuid.New(),Binding:binding,
 Original:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("c",64)},OriginalBytes:100,OriginalMIME:"image/jpeg"}
 publication,err:=delivery.CompileMetadataSourcePublication(scope,ref,raw,"copyright",nil);if err!=nil{t.Fatal(err)}
 parent:=uuid.New();slot:=3;child:=uuid.NewSHA1(parent,[]byte(fmt.Sprintf("child:export:%d",slot)))
 request:=PublishedDeliveryMetadataSpec{ParentRun:parent,ChildRun:child,SlotIndex:slot,EncodedBytes:4096,Binding:binding,Metadata:ref,Policy:"copyright",EncodedJCS:[]byte("{}")}
 workflow,_:=v2input.WorkflowID(child);producer,_:=v2input.TaskID(workflow,"export.encode",0)
 lease:=delivery.MetadataBarrierLease{Tenant:scope.Tenant,ChildRun:child,Expansion:uuid.New(),Token:uuid.New(),ParentTask:producer,Until:time.Now().Add(time.Minute)}
 if err=request.validateLease(lease);err!=nil{t.Fatal(err)}
 spec,err:=request.withPublication(publication);if err!=nil||spec.FrozenSHA!=renderDerivativeBytesSHA(publication.FrozenJCS()){t.Fatal(err)}
 spec.MetadataJCS[0]='!';spec.EncodedJCS[0]='!'
 if publication.SourceJCS()[0]!='{'||request.EncodedJCS[0]!='{'{t.Fatal("mutable composition")}
 for _,mode:=range []string{"binding","source","policy","approval"}{
  current:=request
  switch mode{case "binding":current.Binding.AssetVersionID=uuid.New();case "source":current.Metadata.ArtifactID=uuid.New();case "policy":current.Policy="approved_exif";case "approval":current.ApprovedFields=[]string{"gps"}}
  if _,err:=current.withPublication(publication);err==nil{t.Fatal(mode)}
 }
 pool:=&metadataStagePool{};service:=DeliveryMetadataStage{DB:pool,Objects:metadataStageObjects{},Bucket:"photos"}
 foreign:=request;foreign.SlotIndex++
 if err=service.StagePublished(context.Background(),lease,foreign);err==nil||pool.calls!=0{t.Fatal("foreign slot reached SQL")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err=service.StagePublished(ctx,lease,request);err!=context.Canceled||pool.calls!=0{t.Fatal("cancelled published stage",err)}
}
