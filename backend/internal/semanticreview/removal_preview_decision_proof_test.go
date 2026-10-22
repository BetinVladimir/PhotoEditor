package semanticreview

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestRemovalDecisionProofOwnsExactShownResult(t *testing.T) {
 prepared,targets,request:=decisionProofKindFixture(t,"remove_object")
 shown:=PreviewSelection{Render:TargetDocument{ArtifactID:uuid.New(),SHA256:prepared.PlanRef.SHA256},Evidence:TargetDocument{ArtifactID:uuid.New(),SHA256:prepared.IntentRef.SHA256}}
 request.TargetDecisions[0].Preview=&shown
 proof,err:=BuildDecisionProof(prepared,targets,request)
 if err!=nil {t.Fatal(err)}
 var document decisionDocument
 if json.Unmarshal(proof.Body,&document)!=nil || len(document.Targets)!=1 || document.Targets[0].Preview==nil || *document.Targets[0].Preview!=shown {t.Fatal("shown removal refs lost")}
 owned:=shown
 shown.Render.ArtifactID=uuid.New()
 shown.Evidence.SHA256=prepared.PlanRef.SHA256
 var retained decisionDocument
 if json.Unmarshal(proof.Body,&retained)!=nil || *retained.Targets[0].Preview!=owned {t.Fatal("caller mutated persisted shown refs")}
 next,err:=BuildDecisionProof(prepared,targets,request)
 if err!=nil || next.RequestSHA256==proof.RequestSHA256 || next.SHA256==proof.SHA256 {t.Fatal("changed shown selection reused decision identity",err)}
 // Rejection is an explicit photographer choice with no shown-result binding.
 request.TargetDecisions[0].Decision="reject"
 if _,err:=BuildDecisionProof(prepared,targets,request);err==nil {t.Fatal("reject accepted stale shown approval")}
 request.TargetDecisions[0].Preview=nil
 rejected,err:=BuildDecisionProof(prepared,targets,request)
 if err!=nil || len(rejected.Approved)!=0 || rejected.RequestSHA256==next.RequestSHA256 {t.Fatal("rejection not independently bound",err)}
}

func TestRemovalShownDecisionCannotAliasRenderAndEvidence(t *testing.T) {
 prepared,_,request:=decisionProofKindFixture(t,"remove_object")
 ref:=TargetDocument{ArtifactID:uuid.New(),SHA256:prepared.PlanRef.SHA256}
 request.TargetDecisions[0].Preview=&PreviewSelection{Render:ref,Evidence:ref}
 raw,err:=canonical.Marshal(request);if err!=nil {t.Fatal(err)}
 if _,err:=DecodeApplyDecision(raw);err==nil {t.Fatal("aliased shown removal accepted")}
}

func TestRemovalEachShownReferenceFieldBindsDecisionIdentity(t *testing.T) {
 for _,field:=range []string{"render_id","render_sha","evidence_id","evidence_sha"} {
  t.Run(field,func(t *testing.T) {
   prepared,targets,request:=decisionProofKindFixture(t,"remove_object")
   request.TargetDecisions[0].Preview=&PreviewSelection{Render:TargetDocument{ArtifactID:uuid.New(),SHA256:prepared.PlanRef.SHA256},Evidence:TargetDocument{ArtifactID:uuid.New(),SHA256:prepared.IntentRef.SHA256}}
   first,err:=BuildDecisionProof(prepared,targets,request);if err!=nil {t.Fatal(err)}
   repeat,err:=BuildDecisionProof(prepared,targets,request)
   if err!=nil || repeat.RequestSHA256!=first.RequestSHA256 || repeat.SHA256!=first.SHA256 {t.Fatal("unstable decision",err)}
   shown:=request.TargetDecisions[0].Preview
   differentSHA:=func(value string)string {if value[0]=='0' {return "1"+value[1:]};return "0"+value[1:]}
   switch field {
   case "render_id":shown.Render.ArtifactID=uuid.New()
   case "render_sha":shown.Render.SHA256=differentSHA(shown.Render.SHA256)
   case "evidence_id":shown.Evidence.ArtifactID=uuid.New()
   case "evidence_sha":shown.Evidence.SHA256=differentSHA(shown.Evidence.SHA256)
   }
   changed,err:=BuildDecisionProof(prepared,targets,request)
   if err!=nil || changed.RequestSHA256==first.RequestSHA256 || changed.SHA256==first.SHA256 {t.Fatal("unbound shown field",field,err)}
   var doc decisionDocument
   if json.Unmarshal(changed.Body,&doc)!=nil || doc.Targets[0].Preview==nil || *doc.Targets[0].Preview!=*shown {t.Fatal("different shown refs persisted",field)}
  })
 }
}
