package delivery

import (
 "context"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestBuildSignedAssemblyDocumentsRequiresWholeVerifiedSetBeforePublication(t *testing.T){
 frozen,snapshot,slots:=assemblyFixture(t,1)
 tenant,actor,parent,policy:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 sha:=strings.Repeat("a",64)
 frozen.Policy.Provenance="required";frozen.Policy.SigningPolicyID=policy;frozen.Policy.SigningPolicySHA=sha
 dependencies,err:=CompileSigningDependencies(parent,policy,slots);if err!=nil{t.Fatal(err)}
 dependency:=dependencies[0]
 workflow:=uuid.NewSHA1(tenant,[]byte("provenance:"+actor.String()+":"+dependency.IdempotencyKey))
 signed:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha};manifest:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
 receipt:=SignedDependencyReceipt{Signed:signed,Manifest:manifest,ValidationSHA:sha,
  TaskID:uuid.NewSHA1(workflow,[]byte("provenance.sign:0")),ResultID:uuid.New(),Bucket:"artifacts",
  SignedObject:objectstore.Object{Key:"tenants/test/signed",VersionID:"v1",SHA256:sha,Bytes:42,ContentType:"image/jpeg"},
  ManifestObject:objectstore.Object{Key:"tenants/test/manifest",VersionID:"v2",SHA256:sha,Bytes:13,ContentType:"application/c2pa"}}
 calls:=0
 publish:=func(_ context.Context,_ string,raw []byte)(v2input.Ref,error){calls++;return v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(raw)},nil}
 if _,_,_,err:=BuildAssemblyInput(context.Background(),frozen,snapshot,slots,publish);err==nil||calls!=0{t.Fatal("public off path accepted required")}
 documents,err:=BuildSignedAssemblyDocuments(context.Background(),frozen,snapshot,slots,tenant,actor,parent,dependencies,[]SignedDependencyReceipt{receipt},publish)
 if err!=nil||calls!=1||documents.Assembly.Policy.Provenance!="required"||documents.Closure.PolicyID!=policy||documents.AssemblySHA!=digest(documents.AssemblyJCS)||documents.ClosureSHA!=digest(documents.ClosureJCS){t.Fatal("signed documents",err)}
 bad:=receipt;bad.TaskID=uuid.New();calls=0
 if _,err:=BuildSignedAssemblyDocuments(context.Background(),frozen,snapshot,slots,tenant,actor,parent,dependencies,[]SignedDependencyReceipt{bad},publish);err==nil||calls!=0{t.Fatal("bad signer published shards")}
}
