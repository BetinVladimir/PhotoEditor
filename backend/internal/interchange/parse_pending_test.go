package interchange

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func pendingFixture()(uuid.UUID,auth.Identity,CreateRequest,BundleEvidence,ParseLimits){
 tenant:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
 return tenant,who,CreateRequest{ProjectID:uuid.New(),Direction:"import",Format:"photoeditor_bundle",Bundle:&ref},BundleEvidence{ref,100},ParseLimits{RAMBytes:2<<30,ScratchBytes:1<<30,OutputBytes:1<<30,ExpandedBytes:1024,Files:10}
}
func TestParseSnapshotDigestAndNullOwner(t *testing.T){
 tenant,who,request,evidence,limits:=pendingFixture();plan,err:=CompileParsePlan(tenant,who,request,"key",evidence,limits);if err!=nil{t.Fatal(err)}
 document,err:=plan.CompileSnapshot();if err!=nil{t.Fatal(err)}
 if document.Snapshot.Items[0].OwnerID!=nil||document.Snapshot.Items[0].Artifact!=evidence.Ref||document.Snapshot.Items[0].Contract!="InterchangeArchive"{t.Fatal("invalid artifact snapshot")}
 var value map[string]any;if json.Unmarshal(document.Body,&value)!=nil{t.Fatal("invalid body")};delete(value,"snapshot_digest")
 digest,err:=canonical.Hash(value);if err!=nil||digest!=document.Snapshot.SnapshotDigest{t.Fatal("wrong semantic digest",err)}
 bodyDigest,err:=canonical.Hash(document.Snapshot);if err!=nil||document.Ref.SHA256!=bodyDigest{t.Fatal("wrong body digest")}
 document.Snapshot.Items[0].Artifact.ArtifactID=uuid.New();again,err:=plan.CompileSnapshot();if err!=nil||again.Snapshot.Items[0].Artifact!=evidence.Ref{t.Fatal("snapshot aliases plan")}
 if _,err:=(ParsePlan{}).CompileSnapshot();err==nil{t.Fatal("empty plan snapshot")}
}
func TestParsePendingRecoveryPinsIdentityAndResources(t *testing.T){
 tenant,who,request,evidence,limits:=pendingFixture();raw,err:=EncodeParsePending(tenant,who,request,"key",evidence,limits);if err!=nil{t.Fatal(err)}
 original,err:=CompileParsePlan(tenant,who,request,"key",evidence,limits);if err!=nil{t.Fatal(err)}
 who.SessionID=uuid.New();recovered,err:=DecodeParsePending(tenant,who,request,"key",raw);if err!=nil||recovered.Identity()!=original.Identity()||recovered.OutputBytes()!=original.OutputBytes(){t.Fatal("recovery changed plan",err)}
 for _,change:=range []func(map[string]any){
  func(v map[string]any){v["tenant_id"]=uuid.New().String()},
  func(v map[string]any){v["actor_id"]=uuid.New().String()},
  func(v map[string]any){v["request_sha"]="0"},
  func(v map[string]any){delete(v,"limits")},
  func(v map[string]any){v["extra"]=true},
  func(v map[string]any){v["evidence"].(map[string]any)["bytes"]=0},
 }{
  var value map[string]any;json.Unmarshal(raw,&value);change(value);changed,_:=json.Marshal(value)
  if _,err:=DecodeParsePending(tenant,who,request,"key",changed);err==nil{t.Fatal("invalid recovery accepted",string(changed))}
 }
}
