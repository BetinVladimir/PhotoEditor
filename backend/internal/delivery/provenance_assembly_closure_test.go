package delivery

import (
 "bytes"
 "fmt"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestCompileSignedAssemblyClosureExactSet(t *testing.T){
 sha:=strings.Repeat("a",64)
 tenant,actor,parent,policy:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 makePair:=func(ordinal int)(SigningDependency,SignedDependencyReceipt){
  preset:=uuid.New();export:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
  dependency:=SigningDependency{Ordinal:ordinal,PresetID:preset,ExportID:uuid.New(),Export:export,PolicyID:policy,
   IdempotencyKey:uuid.NewSHA1(parent,[]byte(fmt.Sprintf("%d:%s",ordinal,preset))).String()}
  signed:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha};manifest:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
  workflow:=uuid.NewSHA1(tenant,[]byte("provenance:"+actor.String()+":"+dependency.IdempotencyKey))
  task:=uuid.NewSHA1(workflow,[]byte("provenance.sign:0"))
  receipt:=SignedDependencyReceipt{Signed:signed,Manifest:manifest,ValidationSHA:sha,TaskID:task,ResultID:uuid.New(),Bucket:"artifacts",
   SignedObject:objectstore.Object{Key:fmt.Sprintf("tenants/a/%d/signed",ordinal),VersionID:"v1",SHA256:sha,Bytes:42,ContentType:"image/jpeg"},
   ManifestObject:objectstore.Object{Key:fmt.Sprintf("tenants/a/%d/manifest",ordinal),VersionID:"v2",SHA256:sha,Bytes:13,ContentType:"application/c2pa"}}
  return dependency,receipt
 }
 first,firstReceipt:=makePair(0);second,secondReceipt:=makePair(1)
 deps:=[]SigningDependency{second,first};receipts:=[]SignedDependencyReceipt{secondReceipt,firstReceipt}
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},CanvasHash:sha}
 readySlot:=func(dependency SigningDependency)Slot{
  exportID:=dependency.ExportID;ref:=dependency.Export
  return Slot{Ordinal:dependency.Ordinal,Binding:binding,PresetID:dependency.PresetID,PresetSHA:sha,Required:true,State:"ready",ExportID:&exportID,Export:&ref}
 }
 slots:=[]Slot{readySlot(first),readySlot(second)}
 omission:="OPTIONAL_FAILED"
 slots=append(slots,Slot{Ordinal:2,Binding:binding,PresetID:uuid.New(),PresetSHA:sha,State:"omitted",Reason:&omission})
 closure,raw,hash,err:=CompileSignedAssemblyClosure(tenant,actor,parent,policy,sha,deps,receipts)
 if err!=nil||len(closure.Entries)!=2||closure.Entries[0].Ordinal!=0||hash!=digest(raw){t.Fatal("valid closure",err)}
 reversedDeps:=[]SigningDependency{first,second};reversedReceipts:=[]SignedDependencyReceipt{firstReceipt,secondReceipt}
 _,replayed,replayHash,err:=CompileSignedAssemblyClosure(tenant,actor,parent,policy,sha,reversedDeps,reversedReceipts)
 if err!=nil||!bytes.Equal(raw,replayed)||hash!=replayHash{t.Fatal("noncanonical replay",err)}
 if _,err:=MatchSignedAssemblyClosure(raw,tenant,actor,parent,policy,sha,deps,receipts);err!=nil{t.Fatal("saved closure",err)}
 full,fullRaw,_,err:=CompileSignedAssemblyClosureForSlots(tenant,actor,parent,policy,sha,slots,deps,receipts)
 if err!=nil||len(full.Entries)!=2||!bytes.Equal(raw,fullRaw){t.Fatal("complete matrix",err)}
 if _,err:=MatchSignedAssemblyClosureForSlots(fullRaw,tenant,actor,parent,policy,sha,slots,deps,receipts);err!=nil{t.Fatal("saved matrix",err)}
 if _,_,_,err:=CompileSignedAssemblyClosureForSlots(tenant,actor,parent,policy,sha,slots,deps[:1],receipts[:1]);err==nil{t.Fatal("ready slot omitted")}
 changedSlots:=append([]Slot(nil),slots...);changedSlots[2].Required=true
 if _,_,_,err:=CompileSignedAssemblyClosureForSlots(tenant,actor,parent,policy,sha,changedSlots,deps,receipts);err==nil{t.Fatal("required omission accepted")}
 changedSlots=append([]Slot(nil),slots...);changedSlots[0].Export=&v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
 if _,err:=MatchSignedAssemblyClosureForSlots(fullRaw,tenant,actor,parent,policy,sha,changedSlots,deps,receipts);err==nil{t.Fatal("changed export accepted")}
 if _,err:=MatchSignedAssemblyClosure(append(append([]byte(nil),raw...),[]byte(" {}")...),tenant,actor,parent,policy,sha,deps,receipts);err==nil{t.Fatal("accepted trailing JSON")}
 if _,err:=MatchSignedAssemblyClosure(raw,uuid.New(),actor,parent,policy,sha,deps,receipts);err==nil{t.Fatal("accepted foreign tenant")}
 raw[0]='x';if closure.Entries[0].Signed!=firstReceipt.Signed{t.Fatal("owned closure changed")}
 for _,mode:=range []string{"missing","foreign_policy","wrong_key","duplicate_slot","aliased_artifact","aliased_object","duplicate_task","foreign_task","bad_receipt"}{
  currentDeps:=append([]SigningDependency(nil),deps...)
  currentReceipts:=append([]SignedDependencyReceipt(nil),receipts...)
  switch mode{
  case "missing":currentReceipts=currentReceipts[:1]
  case "foreign_policy":currentDeps[0].PolicyID=uuid.New()
  case "wrong_key":currentDeps[0].IdempotencyKey=uuid.NewString()
  case "duplicate_slot":currentDeps[1]=currentDeps[0]
  case "aliased_artifact":currentReceipts[1].Manifest.ArtifactID=currentReceipts[0].Signed.ArtifactID
  case "aliased_object":currentReceipts[1].ManifestObject.Key=currentReceipts[0].SignedObject.Key
  case "duplicate_task":currentReceipts[1].TaskID=currentReceipts[0].TaskID
  case "foreign_task":currentReceipts[0].TaskID=uuid.New()
  case "bad_receipt":currentReceipts[0].SignedObject.SHA256=strings.Repeat("b",64)
  }
  if _,_,_,err:=CompileSignedAssemblyClosure(tenant,actor,parent,policy,sha,currentDeps,currentReceipts);err==nil{t.Fatal("accepted",mode)}
 }
}
