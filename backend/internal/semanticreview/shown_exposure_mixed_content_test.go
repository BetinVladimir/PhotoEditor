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
func shownExposureMultiBatchFixture(t *testing.T)([]shownExposureContentFixture,PreparedReview,[]LockedTarget,ApplyDecision) {
 t.Helper()
 fs:=[]shownExposureContentFixture{fullShownExposureContentFixture(t),fullShownExposureContentFixture(t)}
 common:=fs[0].physical.linkage.Presentation.Params
 encode:=func(v any)[]byte{b,e:=canonical.Marshal(v);if e!=nil{t.Fatal(e)};return b}
 ref:=func(b []byte)v2input.Ref{sum:=sha256.Sum256(b);return v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}}
 _,template,model,dataset:=preparedDocumentsKindFixture(t,"local_exposure")
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
  var evidence ExposurePreviewEvidence;if err=json.Unmarshal(f.physical.linkage.Evidence.raw,&evidence);err!=nil{t.Fatal(err)}
  evidence.Params=p;f.physical.linkage.Evidence.raw=encode(evidence);f.physical.linkage.Presentation.NumericalEvidence=ref(f.physical.linkage.Evidence.raw)
  f.physical.linkage.Document.raw=encode(f.physical.linkage.Presentation);f.physical.linkage.Document.Source.Ref=ref(f.physical.linkage.Document.raw)
  f.selected.Receipt.NumericalEvidence=f.physical.linkage.Presentation.NumericalEvidence;f.selected.Receipt.Presentation=f.physical.linkage.Document.Source.Ref
  _,f.selected.Receipt.ParamsSHA,err=FreezeExposurePreviewParams(p);if err!=nil{t.Fatal(err)}
  candidate:=p.Candidate.ArtifactID;targets=append(targets,LockedTarget{ID:p.TargetID,AssetVersionID:p.Source.AssetVersionID,CandidateArtifactID:&candidate,State:"proposed",Revision:1})
  request.TargetDecisions=append(request.TargetDecisions,TargetDecision{TargetID:p.TargetID,Decision:"approve",Preview:&PreviewSelection{Render:TargetDocument{ArtifactID:f.selected.Receipt.WorkingImage.ArtifactID,SHA256:f.selected.Receipt.WorkingImage.SHA256},Evidence:TargetDocument{ArtifactID:f.selected.Receipt.Presentation.ArtifactID,SHA256:f.selected.Receipt.Presentation.SHA256}}})
 }
 drafts,err:=CompileApprovedTaskDrafts(fs[0].child,review,targets,request);if err!=nil || len(drafts)!=2{t.Fatal("multi drafts",err)}
 for n:=range fs{for _,draft:=range drafts{if draft.TargetID==fs[n].selected.TargetID{fs[n].approved=draft}}}
 return fs,review,targets,request
}

func TestShownExposureMixedBatchPreflightAndPublication(t *testing.T) {
 for _,mode:=range []string{"mixed","reverse","omit","missing_approval","rejected_input","changed_source","changed_image","changed_candidate","caller_mutation","denied","cancelled"} {
  t.Run(mode,func(t *testing.T){
   fs,review,targets,request:=shownExposureMultiBatchFixture(t)
   request.TargetDecisions[1].Decision="reject"
   request.TargetDecisions[1].Preview=nil
   input:=func(f shownExposureContentFixture)ShownPreparationInput{return ShownPreparationInput{Selected:f.selected,Physical:f.physical,Candidate:f.candidate,Intent:f.intent,Documents:f.docs,Owner:f.owner,Limits:f.limits,Qualification:f.pin}}
   inputs:=[]ShownPreparationInput{input(fs[0])}
   switch mode {
   case "reverse":request.TargetDecisions[0],request.TargetDecisions[1]=request.TargetDecisions[1],request.TargetDecisions[0]
   case "omit":request.TargetDecisions=request.TargetDecisions[:1]
   case "missing_approval":inputs=nil
   case "rejected_input":inputs=append(inputs,input(fs[1]))
   case "changed_source":inputs[0].Documents.Source=append(append([]byte(nil),inputs[0].Documents.Source...), ' ')
   case "changed_image":inputs[0].Physical.image.ImageBytes=append(append([]byte(nil),inputs[0].Physical.image.ImageBytes...), ' ')
   case "changed_candidate":inputs[0].Candidate=append(append([]byte(nil),inputs[0].Candidate...), ' ')
   }
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   approved,copies,decisions,checks:=0,0,0,0
   publication:=exposureBatchPublication{
    approved:func(context.Context,ApplyDecision)error{approved++;return nil},
    copyPublisher:func(in ShownPreparationInput,frozen FrozenShownExposureTask)(v2input.Publisher,error){copies++;if in.Selected.TargetID!=fs[0].selected.TargetID{t.Fatal("rejected copy published")};return &previewPublicationRecorder{},nil},
    decision:func(context.Context,ApplyDecision)(PublishedDecision,error){decisions++;return PublishedDecision{BatchID:uuid.New()},nil},
   }
   p:=fs[0].physical.linkage.Presentation.Params
   result,err:=prepareShownExposureBatch(ctx,p.TenantID,p.IntentID,fs[0].child,review,targets,request,inputs,func(context.Context)error{checks++;if mode=="denied"{return ErrConflict};if mode=="cancelled"{cancel()};if mode=="caller_mutation" && checks==1{inputs[0].Physical.image.ImageBytes[0]='x';inputs[0].Physical.linkage.Document.raw[0]='x';inputs[0].Physical.linkage.Evidence.raw[0]='x';inputs[0].Candidate[0]='x';inputs[0].Intent[0]='x';inputs[0].Documents.Source[0]='x'};return nil},publication)
   valid:=mode=="mixed" || mode=="reverse" || mode=="omit" || mode=="caller_mutation"
   if valid {
    if err!=nil || result.Child!=fs[0].child || len(result.ShownTasks)!=1 || approved!=1 || copies!=1 || decisions!=1 || checks!=5{t.Fatal("mixed exposure refused",err,approved,copies,decisions,checks)}
    if _,err=VerifyShownExposureBatch(p.TenantID,p.IntentID,fs[0].child,review,targets,request,result.ShownTasks);err!=nil{t.Fatal("selected output lost",err)}
   }else{
    if err==nil || result.Child!=uuid.Nil || len(result.ShownTasks)!=0 || approved!=0 || copies!=0 || decisions!=0{t.Fatal("invalid batch published",err,approved,copies,decisions)}
    if mode=="cancelled" && err!=context.Canceled{t.Fatal("cancel lost",err)}
   }
   if targets[1].State!="proposed" || targets[1].Revision!=1{t.Fatal("unapproved target mutated")}
  })
 }
}

func TestShownExposureWholeBatchPreflightBeforeFirstPublication(t *testing.T) {
 for _,mode:=range []string{"two_approvals","reverse_inputs","second_source","second_image","second_candidate","duplicate_input"} {
  t.Run(mode,func(t *testing.T){
   fs,review,targets,request:=shownExposureMultiBatchFixture(t)
   var inputs []ShownPreparationInput
   for _,f:=range fs{inputs=append(inputs,ShownPreparationInput{Selected:f.selected,Physical:f.physical,Candidate:f.candidate,Intent:f.intent,Documents:f.docs,Owner:f.owner,Limits:f.limits,Qualification:f.pin})}
   switch mode{
   case "reverse_inputs":inputs[0],inputs[1]=inputs[1],inputs[0]
   case "second_source":inputs[1].Documents.Source=append(append([]byte(nil),inputs[1].Documents.Source...), ' ')
   case "second_image":inputs[1].Physical.image.ImageBytes=append(append([]byte(nil),inputs[1].Physical.image.ImageBytes...), ' ')
   case "second_candidate":inputs[1].Candidate=append(append([]byte(nil),inputs[1].Candidate...), ' ')
   case "duplicate_input":inputs[1]=inputs[0]
   }
   approved,copies,decisions:=0,0,0
   publication:=exposureBatchPublication{
    approved:func(context.Context,ApplyDecision)error{approved++;return nil},
    copyPublisher:func(ShownPreparationInput,FrozenShownExposureTask)(v2input.Publisher,error){copies++;return &previewPublicationRecorder{},nil},
    decision:func(context.Context,ApplyDecision)(PublishedDecision,error){decisions++;return PublishedDecision{BatchID:uuid.New()},nil},
   }
   p:=fs[0].physical.linkage.Presentation.Params
   result,err:=prepareShownExposureBatch(context.Background(),p.TenantID,p.IntentID,fs[0].child,review,targets,request,inputs,func(context.Context)error{return nil},publication)
   if mode=="two_approvals" || mode=="reverse_inputs"{
    if err!=nil || len(result.ShownTasks)!=2 || approved!=1 || copies!=2 || decisions!=1{t.Fatal("complete batch refused",err,approved,copies,decisions)}
   }else if err==nil || result.Child!=uuid.Nil || len(result.ShownTasks)!=0 || approved!=0 || copies!=0 || decisions!=0{
    t.Fatal("invalid later closure published earlier target",err,approved,copies,decisions)
   }
  })
 }
}
