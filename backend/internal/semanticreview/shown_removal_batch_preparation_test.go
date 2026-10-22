package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
	"testing"
)

func TestShownRemovalBatchPreparationCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := PrepareShownRemovalApplyBatch(ctx, nil, nil, "owned", auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil,
		PreparedReview{}, nil, ApplyDecision{}, nil, nil)
	if err != context.Canceled {
		t.Fatal(err)
	}
}

func TestShownRemovalBatchPreparationRejectsMissingPublicationDependencies(t *testing.T) {
	_, err := PrepareShownRemovalApplyBatch(context.Background(), nil, nil, "owned", auth.Identity{}, uuid.New(), uuid.New(), uuid.New(),
		PreparedReview{}, nil, ApplyDecision{}, nil, func(context.Context) error { t.Fatal("unexpected authority callback"); return nil })
	if err == nil {
		t.Fatal("missing dependencies accepted")
	}
}

func TestShownRemovalPublicationFenceStopsAfterCancelledCallback(t *testing.T) {
 for _,mode:=range []string{"valid","cancel_before","cancel_during","cancel_with_error","denied","nil"} {
  t.Run(mode,func(t *testing.T){
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   calls:=0
   check:=func(context.Context)error {
    calls++
    if mode=="cancel_during" || mode=="cancel_with_error" {cancel()}
    if mode=="denied" || mode=="cancel_with_error" {return ErrConflict}
    return nil
   }
   if mode=="cancel_before" {cancel()}
   if mode=="nil" {check=nil}
   err:=recheckShownRemovalPublication(ctx,check)
   want:=error(nil)
   if mode=="cancel_before" || mode=="cancel_during" || mode=="cancel_with_error" {want=context.Canceled}
   if mode=="denied" {want=ErrConflict}
   if mode=="nil" {want=ErrInvalid}
   if err!=want {t.Fatal("publication fence error mismatch",mode,err,want)}
   expected:=1;if mode=="cancel_before" || mode=="nil" {expected=0}
   if calls!=expected {t.Fatal("unexpected authority callback",calls,expected)}
  })
 }
}

func TestShownRemovalBatchPreflightPublishesNothingForInvalidSelection(t *testing.T) {
 for _,mode:=range []string{"missing_inputs","foreign_target","missing_shown_refs","unverified_physical"} {
  t.Run(mode,func(t *testing.T){
   review,targets,request:=decisionProofKindFixture(t,"remove_object")
   _,_,selected,physical:=shownRemovalCompileFixture(t)
   input:=ShownRemovalPreparationInput{Selected:selected,Physical:physical}
   input.Selected.TargetID=request.TargetDecisions[0].TargetID
   request.TargetDecisions[0].Preview=&PreviewSelection{Render:TargetDocument{ArtifactID:selected.Receipt.WorkingImage.ArtifactID,SHA256:selected.Receipt.WorkingImage.SHA256},Evidence:TargetDocument{ArtifactID:selected.Receipt.Presentation.ArtifactID,SHA256:selected.Receipt.Presentation.SHA256}}
   inputs:=[]ShownRemovalPreparationInput{input}
   if mode=="missing_inputs" {inputs=nil}
   if mode=="foreign_target" {inputs[0].Selected.TargetID=uuid.New()}
   if mode=="missing_shown_refs" {request.TargetDecisions[0].Preview=nil}
   if mode=="unverified_physical" {inputs[0].Physical.verified=false}
   calls:=0
   publication:=removalBatchPublication{
    approved:func(context.Context,ApplyDecision)error{calls++;return nil},
    copyPublisher:func(ShownRemovalPreparationInput,FrozenShownRemovalTask)(v2input.Publisher,error){calls++;return nil,ErrInvalid},
    decision:func(context.Context,ApplyDecision)(PublishedDecision,error){calls++;return PublishedDecision{},ErrInvalid},
   }
   got,err:=prepareShownRemovalBatch(context.Background(),uuid.New(),uuid.New(),uuid.New(),review,targets,request,inputs,func(context.Context)error{calls++;return nil},publication)
   if err==nil || calls!=0 || got.Child!=uuid.Nil || len(got.ShownTasks)!=0 {t.Fatal("invalid batch advanced to publication",mode,err,calls)}
  })
 }
}
