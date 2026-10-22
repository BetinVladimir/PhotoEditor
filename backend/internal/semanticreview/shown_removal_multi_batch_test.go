package semanticreview

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Same-review two-target content fixture; no SQL or physical authority is mocked
// into production. Private verified markers exist only in this test package.
func shownRemovalMultiBatchFixture(t *testing.T)([]shownRemovalContentFixture,PreparedReview,[]LockedTarget,ApplyDecision) {
 t.Helper()
 fs:=[]shownRemovalContentFixture{fullShownRemovalContentFixture(t),fullShownRemovalContentFixture(t)}
 common:=fs[0].physical.linkage.Presentation.Params
 encode:=func(v any)[]byte{b,e:=canonical.Marshal(v);if e!=nil{t.Fatal(e)};return b}
 ref:=func(b []byte)v2input.Ref{sum:=sha256.Sum256(b);return v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}}
 _,template,model,dataset:=preparedDocumentsKindFixture(t,"remove_object")
 sources:=PlanSources{RunID:common.WorkflowID,TaskID:uuid.New(),ResultID:uuid.New(),Revision:1}
 var documents []PlanDocument
 add:=func(contract string,b []byte,r v2input.Ref){s:=PlanSource{Contract:contract,Ref:r,Bytes:int64(len(b)),Bucket:"owned",Key:r.ArtifactID.String(),Version:"v1",MediaType:"application/json"};sources.Objects=append(sources.Objects,s);documents=append(documents,PlanDocument{Source:s,Body:b})}
 var calibration v2input.Ref
 for _,d:=range template{if d.Source.Contract=="Calibration"{calibration=d.Source.Ref;add("Calibration",d.Body,calibration)}}
 var items []v2input.SessionSnapshotItem
 var candidates []v2input.SemanticCandidate
 var refs []v2input.Ref
 for n:=range fs {
  f:=&fs[n];p:=f.physical.linkage.Presentation.Params
  p.TenantID,p.IntentID,p.WorkflowID,p.SnapshotID=common.TenantID,common.IntentID,common.WorkflowID,common.SnapshotID
  p.Intent=common.Intent;p.WorkflowRevision,p.TargetRevision="1","1"
  f.child=fs[0].child;f.intent=append([]byte(nil),fs[0].intent...);f.docs.Reference=append([]byte(nil),fs[0].docs.Reference...)
  f.physical.linkage.Presentation.Params=p
  items=append(items,v2input.SessionSnapshotItem{Ordinal:n,Binding:p.Source,WorkingImage:p.Source.Source,Recipe:p.Source.Source})
  var candidate v2input.SemanticCandidate;if err:=json.Unmarshal(f.candidate,&candidate);err!=nil{t.Fatal(err)}
  candidates=append(candidates,candidate);refs=append(refs,p.Candidate);add("TransferCandidate",f.candidate,p.Candidate)
 }
 snapshot,snapshotRaw,err:=v2input.CompilePinnedSessionInputSnapshot(common.SnapshotID,items);if err!=nil{t.Fatal(err)}
 add("InputSnapshot",snapshotRaw,ref(snapshotRaw));add("EditIntent",fs[0].intent,common.Intent)
 assembly:=encode(v2input.SemanticAssembleParams{SchemaVersion:1,Intent:common.Intent,Candidates:refs,Calibration:calibration,ModelLockSHA:model});add("Params",assembly,ref(assembly))
 plan:=encode(v2input.SemanticPlan{SchemaVersion:1,IntentSHA:common.Intent.SHA256,SnapshotDigest:snapshot.SnapshotDigest,Candidates:candidates,ModelLockSHA:model,Calibration:calibration});planRef:=ref(plan);add("SemanticPlan",plan,planRef)
 review,err:=PrepareReviewDocuments(sources,documents,model,dataset);if err!=nil{t.Fatal("multi review",err)}
 var targets []LockedTarget
 request:=ApplyDecision{PlanSHA:planRef.SHA256,ExpectedRevision:"1"}
 for n:=range fs {
  f:=&fs[n];p:=f.physical.linkage.Presentation.Params;p.Plan=planRef;f.physical.linkage.Presentation.Params=p
  var evidence RemovalPreviewEvidence;if err=json.Unmarshal(f.physical.linkage.Evidence.raw,&evidence);err!=nil{t.Fatal(err)}
  evidence.Params=p;f.physical.linkage.Evidence.raw=encode(evidence);f.physical.linkage.Presentation.NumericalEvidence=ref(f.physical.linkage.Evidence.raw)
  f.physical.linkage.Document.raw=encode(f.physical.linkage.Presentation);f.physical.linkage.Document.Source.Ref=ref(f.physical.linkage.Document.raw)
  f.selected.Receipt.NumericalEvidence=f.physical.linkage.Presentation.NumericalEvidence;f.selected.Receipt.Presentation=f.physical.linkage.Document.Source.Ref
  _,f.selected.Receipt.ParamsSHA,err=FreezeRemovalPreviewParams(p);if err!=nil{t.Fatal(err)}
  candidate:=p.Candidate.ArtifactID;targets=append(targets,LockedTarget{ID:p.TargetID,AssetVersionID:p.Source.AssetVersionID,CandidateArtifactID:&candidate,State:"proposed",Revision:1})
  request.TargetDecisions=append(request.TargetDecisions,TargetDecision{TargetID:p.TargetID,Decision:"approve",Preview:&PreviewSelection{Render:TargetDocument{ArtifactID:f.selected.Receipt.WorkingImage.ArtifactID,SHA256:f.selected.Receipt.WorkingImage.SHA256},Evidence:TargetDocument{ArtifactID:f.selected.Receipt.Presentation.ArtifactID,SHA256:f.selected.Receipt.Presentation.SHA256}}})
 }
 drafts,err:=CompileApprovedTaskDrafts(fs[0].child,review,targets,request);if err!=nil || len(drafts)!=2{t.Fatal("multi drafts",err)}
 for n:=range fs{for _,draft:=range drafts{if draft.TargetID==fs[n].selected.TargetID{fs[n].approved=draft}}}
 return fs,review,targets,request
}

func TestShownRemovalMultiBatchPreflightAndPublication(t *testing.T) {
 for _,mode:=range []string{"valid","reverse","second_image","second_candidate","duplicate","second_factory"} {
  t.Run(mode,func(t *testing.T){
   fs,review,targets,request:=shownRemovalMultiBatchFixture(t)
   var inputs []ShownRemovalPreparationInput
   for _,f:=range fs{inputs=append(inputs,ShownRemovalPreparationInput{Selected:f.selected,Physical:f.physical,Candidate:f.candidate,Intent:f.intent,Documents:f.docs,Owner:f.owner,Limits:f.limits,Qualification:f.pin})}
   switch mode{case "reverse":inputs[0],inputs[1]=inputs[1],inputs[0];case "second_image":inputs[1].Physical.image.ImageBytes=append(inputs[1].Physical.image.ImageBytes,' ');case "second_candidate":inputs[1].Candidate=append(inputs[1].Candidate,' ');case "duplicate":inputs[1]=inputs[0]}
   approved,copies,decisions:=0,0,0
   publication:=removalBatchPublication{
    approved:func(context.Context,ApplyDecision)error{approved++;return nil},
    copyPublisher:func(ShownRemovalPreparationInput,FrozenShownRemovalTask)(v2input.Publisher,error){copies++;if mode=="second_factory" && copies==2{return nil,ErrUnavailable};return &previewPublicationRecorder{},nil},
    decision:func(context.Context,ApplyDecision)(PublishedDecision,error){decisions++;return PublishedDecision{BatchID:uuid.New()},nil},
   }
   p:=fs[0].physical.linkage.Presentation.Params
   got,err:=prepareShownRemovalBatch(context.Background(),p.TenantID,p.IntentID,fs[0].child,review,targets,request,inputs,func(context.Context)error{return nil},publication)
   if mode=="valid" || mode=="reverse"{if err!=nil || len(got.ShownTasks)!=2 || approved!=1 || copies!=2 || decisions!=1{t.Fatal("multi batch refused",err,approved,copies,decisions)}}else{
    if err==nil || got.Child!=uuid.Nil || len(got.ShownTasks)!=0 || decisions!=0{t.Fatal("partial batch escaped",mode,err)}
    if mode!="second_factory" && (approved!=0 || copies!=0){t.Fatal("invalid second target published first",mode,approved,copies)}
    if mode=="second_factory" && (approved!=1 || copies!=2){t.Fatal("failure position changed",approved,copies)}
   }
  })
 }
}


func TestShownRemovalMixedDecisionsPublishCopiesOnlyForApproval(t *testing.T) {
 for _, mode := range []string{"valid", "reverse_decisions", "omit_second", "extra_rejected_input", "missing_approved_input", "rejected_preview"} {
  t.Run(mode, func(t *testing.T) {
   fs, review, targets, request := shownRemovalMultiBatchFixture(t)
   approvedID := request.TargetDecisions[0].TargetID
   request.TargetDecisions[1].Decision = "reject"
   request.TargetDecisions[1].Preview = nil
   makeInput := func(f shownRemovalContentFixture) ShownRemovalPreparationInput {
    return ShownRemovalPreparationInput{Selected:f.selected, Physical:f.physical, Candidate:f.candidate,
     Intent:f.intent, Documents:f.docs, Owner:f.owner, Limits:f.limits, Qualification:f.pin}
   }
   inputs := []ShownRemovalPreparationInput{makeInput(fs[0])}
   switch mode {
   case "reverse_decisions": request.TargetDecisions[0], request.TargetDecisions[1] = request.TargetDecisions[1], request.TargetDecisions[0]
   case "omit_second": request.TargetDecisions = request.TargetDecisions[:1]
   case "extra_rejected_input": inputs = append(inputs, makeInput(fs[1]))
   case "missing_approved_input": inputs[0] = makeInput(fs[1])
   case "rejected_preview": request.TargetDecisions[1].Preview = &PreviewSelection{}
   }
   approved, copies, decisions := 0, 0, 0
   checkRequest := func(value ApplyDecision) {
    t.Helper()
    if len(value.TargetDecisions) != len(request.TargetDecisions) { t.Fatal("explicit decisions dropped") }
    for _, choice := range value.TargetDecisions {
     if choice.Decision == "reject" && choice.Preview != nil { t.Fatal("rejection requires preview") }
    }
   }
   publication := removalBatchPublication{
    approved:func(_ context.Context, value ApplyDecision) error { approved++; checkRequest(value); return nil },
    copyPublisher:func(input ShownRemovalPreparationInput, _ FrozenShownRemovalTask) (v2input.Publisher,error) {
     copies++
     if input.Selected.TargetID != approvedID { t.Fatal("copy created for unapproved target") }
     return &previewPublicationRecorder{},nil
    },
    decision:func(_ context.Context, value ApplyDecision) (PublishedDecision,error) {
     decisions++; checkRequest(value); return PublishedDecision{BatchID:uuid.New()},nil
    },
   }
   p := fs[0].physical.linkage.Presentation.Params
   got, err := prepareShownRemovalBatch(context.Background(), p.TenantID, p.IntentID, fs[0].child,
    review, targets, request, inputs, func(context.Context) error { return nil }, publication)
   valid := mode == "valid" || mode == "reverse_decisions" || mode == "omit_second"
   if valid {
    if err != nil || len(got.ShownTasks) != 1 || got.ShownTasks[0].preview.TargetID != approvedID ||
     len(got.Targets) != len(targets) || approved != 1 || copies != 1 || decisions != 1 {
     t.Fatal("mixed/partial decisions did not preserve exact batch", mode, err, approved, copies, decisions)
    }
   } else if err == nil || got.Child != uuid.Nil || len(got.ShownTasks) != 0 || approved != 0 || copies != 0 || decisions != 0 {
    t.Fatal("invalid mixed preflight published partial batch", mode, err, approved, copies, decisions)
   }
  })
 }
}


func TestMixedDecisionProofAndDraftsPreserveExplicitChoices(t *testing.T) {
 for _, mode := range []string{"valid", "reverse", "omit", "reject_preview"} {
  t.Run(mode, func(t *testing.T) {
   fs, review, targets, request := shownRemovalMultiBatchFixture(t)
   approvedID := request.TargetDecisions[0].TargetID
   request.TargetDecisions[1].Decision = "reject"
   request.TargetDecisions[1].Preview = nil
   switch mode {
   case "reverse": request.TargetDecisions[0],request.TargetDecisions[1]=request.TargetDecisions[1],request.TargetDecisions[0]
   case "omit": request.TargetDecisions=request.TargetDecisions[:1]
   case "reject_preview": request.TargetDecisions[1].Preview=&PreviewSelection{}
   }
   proof, err := BuildDecisionProof(review, targets, request)
   if mode == "reject_preview" {
    if err == nil || len(proof.Body) != 0 || len(proof.Approved) != 0 { t.Fatal("invalid rejection produced proof",err) }
    return
   }
   if err != nil || len(proof.Approved)!=1 || proof.Approved[0]!=approvedID { t.Fatal("wrong approved proof set",err) }
   var body decisionDocument
   if err=json.Unmarshal(proof.Body,&body);err!=nil { t.Fatal(err) }
   if len(body.Targets)!=len(request.TargetDecisions) { t.Fatal("explicit decision omitted or invented") }
   for _, target := range body.Targets {
    if target.TargetID==approvedID {
     if target.Decision!="approve" || target.Preview==nil { t.Fatal("approved shown choice lost") }
    } else if target.Decision!="reject" || target.Preview!=nil { t.Fatal("rejection acquired preview") }
   }
   drafts,err:=CompileApprovedTaskDrafts(fs[0].child,review,targets,request)
   if err!=nil || len(drafts)!=1 || drafts[0].TargetID!=approvedID { t.Fatal("unapproved target produced task draft",err) }
  })
 }
}
