package delivery

import (
 "context"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

type deliverySigningAuthority struct{}
func(deliverySigningAuthority)CheckSigningOperator(context.Context,provenance.SigningPolicy)error{return nil}
type deliverySigningSource struct{evidence provenance.SigningExportEvidence;calls int}
func(s *deliverySigningSource)ResolveSigningExport(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(provenance.SigningExportEvidence,error){s.calls++;return s.evidence,nil}

func TestPrepareSigningDependenciesRechecksLiveExportAndPolicy(t *testing.T){
 sha:=strings.Repeat("a",64);now:=time.Date(2026,10,13,12,0,0,0,time.UTC)
 policyID:=uuid.New()
 policy:=provenance.SigningPolicy{Schema:1,ID:policyID,Revision:"1",SDKLockSHA:sha,KeyHandle:"operator-key",CertificateChainSHA:sha,TimestampMode:"off",AllowedMIME:[]string{"image/jpeg"},TrustBundleSHA:sha,RevocationMode:"off"}
 policyRaw,err:=canonical.Marshal(policy);if err!=nil{t.Fatal(err)}
 registry,err:=provenance.NewSigningPolicyRegistry(provenance.SigningInstallation{SDKLockSHA:sha,CertificateChainSHA:sha,TrustBundleSHA:sha,KeyHandles:[]string{"operator-key"},Policies:[]provenance.InstalledSigningPolicy{{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(policyRaw)},Body:policyRaw,NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour)}}},deliverySigningAuthority{})
 if err!=nil{t.Fatal(err)}
 exportID:=uuid.New();exportRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
 lineageRaw,err:=canonical.Marshal(provenance.SigningLineage{Schema:1,SourceHashes:[]string{sha},Actions:[]provenance.SigningAction{}});if err!=nil{t.Fatal(err)}
 project:=uuid.New();source:=&deliverySigningSource{evidence:provenance.SigningExportEvidence{Project:project,ExportID:exportID,Export:exportRef,MIME:"image/jpeg",Lineage:v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(lineageRaw)},LineageBody:lineageRaw}}
 slot:=Slot{Ordinal:0,Binding:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},CanvasHash:sha},PresetID:uuid.New(),PresetSHA:sha,Required:true,State:"ready",ExportID:&exportID,Export:&exportRef}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant,parent:=uuid.New(),uuid.New()
 frozen:=FrozenInput{SchemaVersion:1,Policy:Policy{Provenance:"required",SigningPolicyID:policyID,SigningPolicySHA:digest(policyRaw)}}
 first,err:=PrepareSigningDependencies(context.Background(),who,tenant,project,parent,frozen,[]Slot{slot},source,registry,now)
 if err!=nil||len(first)!=1||source.calls!=2{t.Fatal("signing dependency not prepared",err,source.calls)}
 again,err:=PrepareSigningDependencies(context.Background(),who,tenant,project,parent,frozen,[]Slot{slot},source,registry,now)
 if err!=nil||len(again)!=1||first[0].WorkflowID()!=again[0].WorkflowID(){t.Fatal("signing retry identity changed",err)}
 wrongChoice:=frozen;wrongChoice.Policy.SigningPolicySHA=strings.Repeat("b",64)
 if _,err:=PrepareSigningDependencies(context.Background(),who,tenant,project,parent,wrongChoice,[]Slot{slot},source,registry,now);err==nil{t.Fatal("changed frozen policy SHA accepted")}
 source.evidence.Project=uuid.New()
 if _,err:=PrepareSigningDependencies(context.Background(),who,tenant,project,parent,frozen,[]Slot{slot},source,registry,now);err==nil{t.Fatal("foreign project accepted")}
 source.evidence.Project=project;source.evidence.Export.ArtifactID=uuid.New()
 if _,err:=PrepareSigningDependencies(context.Background(),who,tenant,project,parent,frozen,[]Slot{slot},source,registry,now);err==nil{t.Fatal("substituted export accepted")}
 source.calls=0;slot.State="pending"
 if _,err:=PrepareSigningDependencies(context.Background(),who,tenant,project,parent,frozen,[]Slot{slot},source,registry,now);err==nil||source.calls!=0{t.Fatal("invalid slot reached signer",err)}
}
