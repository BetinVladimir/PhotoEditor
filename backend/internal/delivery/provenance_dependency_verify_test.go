package delivery

import (
 "context"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

func TestVerifySigningDependencyCurrentRechecksReceipt(t *testing.T){
 sha:=strings.Repeat("a",64);now:=time.Date(2026,10,13,12,0,0,0,time.UTC)
 policyID:=uuid.New();policy:=provenance.SigningPolicy{Schema:1,ID:policyID,Revision:"1",SDKLockSHA:sha,KeyHandle:"operator-key",CertificateChainSHA:sha,TimestampMode:"off",AllowedMIME:[]string{"image/jpeg"},TrustBundleSHA:sha,RevocationMode:"off"}
 raw,err:=canonical.Marshal(policy);if err!=nil{t.Fatal(err)}
 registry,err:=provenance.NewSigningPolicyRegistry(provenance.SigningInstallation{SDKLockSHA:sha,CertificateChainSHA:sha,TrustBundleSHA:sha,KeyHandles:[]string{"operator-key"},Policies:[]provenance.InstalledSigningPolicy{{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(raw)},Body:raw,NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour)}}},deliverySigningAuthority{});if err!=nil{t.Fatal(err)}
 tenant,project,parent,exportID:=uuid.New(),uuid.New(),uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 original:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
 lineageRaw,err:=canonical.Marshal(provenance.SigningLineage{Schema:1,SourceHashes:[]string{sha},Actions:[]provenance.SigningAction{}});if err!=nil{t.Fatal(err)}
 source:=&deliverySigningSource{evidence:provenance.SigningExportEvidence{Project:project,ExportID:exportID,Export:original,MIME:"image/jpeg",Lineage:v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(lineageRaw)},LineageBody:lineageRaw}}
 dep:=SigningDependency{Ordinal:0,PresetID:uuid.New(),ExportID:exportID,Export:original,PolicyID:policyID}
 dep.IdempotencyKey=uuid.NewSHA1(parent,[]byte("0:"+dep.PresetID.String())).String()
 tenantRegistry,err:=registry.ForTenant(tenant);if err!=nil{t.Fatal(err)}
 intent,err:=provenance.PrepareSigningIntent(context.Background(),who,tenant,exportID,policyID,dep.IdempotencyKey,source,tenantRegistry,now);if err!=nil{t.Fatal(err)}
 signedRaw,manifestRaw:=[]byte("signed-jpeg"),[]byte("binary-c2pa")
 signedID,manifestID:=uuid.New(),uuid.New();prefix:="tenants/"+tenant.String()+"/tasks/"+intent.TaskID().String()+"/attempts/1/fences/2/"
 receipt:=SignedDependencyReceipt{Signed:v2input.Ref{ArtifactID:signedID,SHA256:signingObjectHash(signedRaw)},Manifest:v2input.Ref{ArtifactID:manifestID,SHA256:signingObjectHash(manifestRaw)},ValidationSHA:signingObjectHash([]byte("validated")),TaskID:intent.TaskID(),ResultID:uuid.New(),Bucket:"photos"}
 receipt.SignedObject=objectstore.Object{Key:prefix+signedID.String(),VersionID:"version-s",SHA256:receipt.Signed.SHA256,ContentType:"image/jpeg",Bytes:int64(len(signedRaw))}
 receipt.ManifestObject=objectstore.Object{Key:prefix+manifestID.String(),VersionID:"version-m",SHA256:receipt.Manifest.SHA256,ContentType:"application/c2pa",Bytes:int64(len(manifestRaw))}
 objects:=&signedReceiptObjectStub{contents:map[string][]byte{receipt.SignedObject.Key:signedRaw,receipt.ManifestObject.Key:manifestRaw}}
 calls:=0;read:=func(context.Context)(SignedDependencyReceipt,bool,error){calls++;return receipt,true,nil}
 got,err:=VerifySigningDependencyCurrent(context.Background(),who,tenant,project,parent,dep,intent,digest(raw),registry,func()time.Time{return now},objects,read)
 if err!=nil||got!=receipt||calls!=2||objects.calls!=2{t.Fatal("signed dependency verification",err,calls,objects.calls)}
 calls=0;objects.calls=0
 changed:=func(context.Context)(SignedDependencyReceipt,bool,error){calls++;if calls==2{other:=receipt;other.ValidationSHA=sha;return other,true,nil};return receipt,true,nil}
 if _,err:=VerifySigningDependencyCurrent(context.Background(),who,tenant,project,parent,dep,intent,digest(raw),registry,func()time.Time{return now},objects,changed);err==nil||calls!=2{t.Fatal("changed receipt accepted",err,calls)}
 calls=0;objects.calls=0
 if _,err:=VerifySigningDependencyCurrent(context.Background(),who,tenant,project,uuid.New(),dep,intent,digest(raw),registry,func()time.Time{return now},objects,read);err==nil||calls!=0||objects.calls!=0{t.Fatal("foreign parent reached signed evidence",err)}
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},CanvasHash:sha}
 slot:=Slot{Ordinal:dep.Ordinal,Binding:binding,PresetID:dep.PresetID,PresetSHA:sha,Required:true,State:"ready",ExportID:&exportID,Export:&original}
 verified:=0
 verifier:=func(_ context.Context,dependency SigningDependency,planned provenance.FrozenSigningIntent)(SignedDependencyReceipt,error){
  verified++;if dependency!=dep||planned.TaskID()!=intent.TaskID(){t.Fatal("dependency intent substitution")};return receipt,nil
 }
 collected,adopted,err:=collectSignedClosureEvidence(context.Background(),parent,policyID,[]Slot{slot},
  []provenance.FrozenSigningIntent{intent},verifier)
 if err!=nil||len(collected)!=1||len(adopted)!=1||verified!=1{t.Fatal("whole signed set not collected",err)}
 verified=0
 if _,_,err:=collectSignedClosureEvidence(context.Background(),parent,policyID,[]Slot{slot},nil,verifier);err==nil||verified!=0{t.Fatal("missing signer intent accepted")}
 altered:=func(context.Context,SigningDependency,provenance.FrozenSigningIntent)(SignedDependencyReceipt,error){
  verified++;other:=receipt;other.TaskID=uuid.New();return other,nil
 }
 verified=0
 if _,_,err:=collectSignedClosureEvidence(context.Background(),parent,policyID,[]Slot{slot},
  []provenance.FrozenSigningIntent{intent},altered);err==nil||verified!=1{t.Fatal("foreign signer task accepted")}
}
