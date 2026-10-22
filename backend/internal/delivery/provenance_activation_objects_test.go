package delivery

import (
 "context"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestSignedActivationInventoryExactRowsAndPhysicalBytes(t *testing.T){
 tenant,actor,parent,policy,task:=uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New()
 signedID,manifestID:=uuid.New(),uuid.New()
 signedRaw,manifestRaw:=[]byte("signed-image"),[]byte("c2pa-manifest")
 signedSHA,manifestSHA:=signingObjectHash(signedRaw),signingObjectHash(manifestRaw)
 prefix:="tenants/"+tenant.String()+"/tasks/"+task.String()+"/attempts/1/fences/2/"
 signed:=SignedActivationObject{ArtifactID:signedID,Bucket:"photos",Object:objectstore.Object{
  Key:prefix+signedID.String(),VersionID:"v1",SHA256:signedSHA,
  ContentType:"image/jpeg",Bytes:int64(len(signedRaw))}}
 manifest:=SignedActivationObject{ArtifactID:manifestID,Bucket:"photos",Object:objectstore.Object{
  Key:prefix+manifestID.String(),VersionID:"v2",SHA256:manifestSHA,
  ContentType:"application/c2pa",Bytes:int64(len(manifestRaw))}}
 closure:=SignedAssemblyClosure{SchemaVersion:1,TenantID:tenant,ActorID:actor,
  ParentID:parent,PolicyID:policy,PolicySHA:signingObjectHash([]byte("policy")),
  Entries:[]SignedAssemblyEntry{{TaskID:task,ResultID:uuid.New(),
   Signed:v2input.Ref{ArtifactID:signedID,SHA256:signedSHA},
   Manifest:v2input.Ref{ArtifactID:manifestID,SHA256:manifestSHA},
   ValidationSHA:signingObjectHash([]byte("validation"))}}}
 objects:=&signedReceiptObjectStub{contents:map[string][]byte{
  signed.Object.Key:signedRaw,manifest.Object.Key:manifestRaw}}
 renews:=0
 renew:=func(context.Context)error{renews++;return nil}
 inventory:=[]SignedActivationObject{signed,manifest}
 if err:=VerifySignedActivationInventory(context.Background(),tenant,"photos",closure,
  inventory,objects,renew);err!=nil||objects.calls!=2||renews!=2{t.Fatal(err,objects.calls,renews)}
 if !SignedActivationInventoryEqual(inventory,append([]SignedActivationObject(nil),inventory...)){
  t.Fatal("stable inventory changed")
 }
 missing:=inventory[:1]
 if err:=VerifySignedActivationInventory(context.Background(),tenant,"photos",closure,
  missing,objects,renew);err==nil{t.Fatal("missing manifest accepted")}
 changed:=append([]SignedActivationObject(nil),inventory...)
 changed[0].Object.VersionID="changed"
 if SignedActivationInventoryEqual(inventory,changed){t.Fatal("changed version accepted")}
 mutableInventory:=append([]SignedActivationObject(nil),inventory...)
 mutableClosure:=closure;mutableClosure.Entries=append([]SignedAssemblyEntry(nil),closure.Entries...)
 mutate:=func(context.Context)error{
  mutableInventory[0].Object.VersionID="substituted-after-renew"
  mutableClosure.Entries[0].Signed.SHA256=signingObjectHash([]byte("wrong"))
  return nil
 }
 if err:=VerifySignedActivationInventory(context.Background(),tenant,"photos",mutableClosure,mutableInventory,objects,mutate);err!=nil{t.Fatal("callback changed verified invocation",err)}
 for _,final:=range []bool{false,true}{
  call:=0;ctx,cancel:=context.WithCancel(context.Background());before:=objects.calls
  cancelRenew:=func(context.Context)error{call++;if !final||call==2{cancel()};return nil}
  err:=VerifySignedActivationInventory(ctx,tenant,"photos",closure,inventory,objects,cancelRenew);cancel()
  if err!=context.Canceled||!final&&objects.calls!=before{t.Fatal("renew cancellation lost",final,err,objects.calls-before)}
 }
 objects.contents[signed.Object.Key]=[]byte("substituted!")
 if err:=VerifySignedActivationInventory(context.Background(),tenant,"photos",closure,
  inventory,objects,renew);err==nil{t.Fatal("substituted bytes accepted")}
}
