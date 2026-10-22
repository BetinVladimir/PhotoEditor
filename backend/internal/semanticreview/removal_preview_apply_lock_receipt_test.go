package semanticreview

import (
 "context"
 "fmt"
 "strconv"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type removalSelectionTx struct {
 pgx.Tx
 t *testing.T
 mode string
 calls int
 callback func()
 failAt int
 ids []uuid.UUID
}
func (tx *removalSelectionTx) QueryRow(_ context.Context, query string,args ...any) pgx.Row {
 tx.calls++
 tx.ids=append(tx.ids,args[5].(uuid.UUID))
 if tx.callback!=nil {tx.callback()}
 if !strings.Contains(query,"photo.lock_semantic_removal_preview_selection(") || len(args)!=13 {tx.t.Fatal("foreign selection query",query)}
 run:=uuid.New()
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 r:=RemovalPreviewReceipt{SchemaVersion:1,ParamsSHA:strings.Repeat("a",64),TaskID:uuid.NewSHA1(run,[]byte("semantic.preview_remove_object:"+args[5].(uuid.UUID).String())),ResultID:uuid.New(),Attempt:1,Fence:1,
  WorkingImage:v2input.Ref{ArtifactID:args[9].(uuid.UUID),SHA256:args[10].(string)},Presentation:v2input.Ref{ArtifactID:args[11].(uuid.UUID),SHA256:args[12].(string)},Pixels:ref(),NumericalEvidence:ref()}
 switch tx.mode {
 case "sql_error":if tx.failAt==0 || tx.calls==tx.failAt {return cleanupReadRow{err:ErrConflict}}
 case "run":run=uuid.Nil
 case "schema":r.SchemaVersion=2
 case "task":r.TaskID=uuid.New()
 case "result":r.ResultID=uuid.Nil
 case "attempt":r.Attempt=0
 case "fence":r.Fence=0
 case "params":r.ParamsSHA="bad"
 case "render":r.WorkingImage.ArtifactID=uuid.New()
 case "presentation":r.Presentation.SHA256=strings.Repeat("b",64)
 case "pixels":r.Pixels.ArtifactID=uuid.Nil
 case "evidence":r.NumericalEvidence.SHA256="bad"
 case "alias":r.Pixels=r.WorkingImage
 }
 raw,err:=canonical.Marshal(r);if err!=nil {tx.t.Fatal(err)}
 if tx.mode=="unknown" {raw=append([]byte(`{"extra":true,`),raw[1:]...)}
 return cleanupReadRow{values:[]any{run,raw}}
}
func TestRemovalSelectionStrictReceiptAndWholeInventory(t *testing.T) {
 for _,mode:=range []string{"valid","sql_error","run","schema","task","result","attempt","fence","params","render","presentation","pixels","evidence","alias","unknown","missing_preview","target_version","target_revision","target_child"} {
  t.Run(mode,func(t *testing.T) {
   prepared,targets,request:=decisionProofKindFixture(t,"remove_object")
   request.TargetDecisions[0].Preview=&PreviewSelection{Render:TargetDocument{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},Evidence:TargetDocument{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
   if mode=="missing_preview" {request.TargetDecisions[0].Preview=nil}
   for i:=range targets {
    if targets[i].ID!=request.TargetDecisions[0].TargetID {continue}
    switch mode {
    case "target_version":targets[i].AssetVersionID=uuid.New()
    case "target_revision":targets[i].Revision++
    case "target_child":id:=uuid.New();targets[i].ChildRunID=&id
    }
   }
   tx:=&removalSelectionTx{t:t,mode:mode}
   got,err:=LockApplyRemovalPreviewSelections(context.Background(),tx,uuid.New(),uuid.New(),uuid.New(),uuid.New(),prepared,targets,request)
   if mode=="valid" {
    if err!=nil || len(got)!=1 || got[0].TargetID!=request.TargetDecisions[0].TargetID || tx.calls!=1 {t.Fatal("valid own selection",got,err,tx.calls)}
   } else if err==nil || got!=nil {t.Fatal("invalid selection escaped",mode,got,err)}
   if (mode=="missing_preview" || strings.HasPrefix(mode,"target_")) && tx.calls!=0 {t.Fatal("invalid inventory reached SQL",mode,tx.calls)}
  })
 }
}

func TestRemovalSelectionOwnsRequestAcrossSQLCallback(t *testing.T) {
 for _,mode:=range []string{"mutated_caller","cancelled_callback","reject_only"} {
  t.Run(mode,func(t *testing.T) {
   prepared,targets,request:=decisionProofKindFixture(t,"remove_object")
   shown:=PreviewSelection{Render:TargetDocument{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},Evidence:TargetDocument{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)}}
   request.TargetDecisions[0].Preview=&shown
   expected:=shown
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   tx:=&removalSelectionTx{t:t}
   tx.callback=func(){
    if mode=="cancelled_callback" {cancel();return}
    shown.Render.ArtifactID=uuid.New()
    shown.Evidence.SHA256=strings.Repeat("c",64)
    request.TargetDecisions[0].Decision="reject"
   }
   if mode=="reject_only" {request.TargetDecisions[0].Decision="reject";request.TargetDecisions[0].Preview=nil}
   got,err:=LockApplyRemovalPreviewSelections(ctx,tx,uuid.New(),uuid.New(),uuid.New(),uuid.New(),prepared,targets,request)
   switch mode {
   case "mutated_caller":
    if err!=nil || len(got)!=1 || got[0].Receipt.WorkingImage.ArtifactID!=expected.Render.ArtifactID || got[0].Receipt.WorkingImage.SHA256!=expected.Render.SHA256 || got[0].Receipt.Presentation.ArtifactID!=expected.Evidence.ArtifactID || got[0].Receipt.Presentation.SHA256!=expected.Evidence.SHA256 {t.Fatal("caller changed frozen selection",got,err)}
   case "cancelled_callback":if err!=context.Canceled || got!=nil {t.Fatal("cancelled selection returned proof",got,err)}
   case "reject_only":if err!=nil || len(got)!=0 || tx.calls!=0 {t.Fatal("rejection acquired preview authority",got,err,tx.calls)}
   }
  })
 }
}

func TestRemovalMultiSelectionReturnsNoPartialProof(t *testing.T) {
 for _,failAt:=range []int{0,1,2,3} {
  t.Run(fmt.Sprint(failAt),func(t *testing.T) {
   sources,documents,model,dataset:=preparedDocumentsKindFixture(t,"remove_object",true)
   prepared,err:=PrepareReviewDocuments(sources,documents,model,dataset);if err!=nil {t.Fatal(err)}
   targets:=[]LockedTarget{}
   request:=ApplyDecision{PlanSHA:prepared.PlanRef.SHA256,ExpectedRevision:strconv.FormatInt(sources.Revision,10)}
   for _,seed:=range prepared.Targets {
    candidate:=seed.CandidateArtifactID
    target:=LockedTarget{ID:uuid.New(),AssetVersionID:seed.AssetVersionID,CandidateArtifactID:&candidate,State:"proposed",Revision:1}
    targets=append(targets,target)
    request.TargetDecisions=append(request.TargetDecisions,TargetDecision{TargetID:target.ID,Decision:"approve",Preview:&PreviewSelection{Render:TargetDocument{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},Evidence:TargetDocument{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)}}})
   }
   if len(targets)!=3 {t.Fatal("three own targets required")}
   tx:=&removalSelectionTx{t:t,failAt:failAt}
   if failAt>0 {tx.mode="sql_error"}
   got,err:=LockApplyRemovalPreviewSelections(context.Background(),tx,uuid.New(),uuid.New(),uuid.New(),uuid.New(),prepared,targets,request)
   if failAt==0 {
    if err!=nil || len(got)!=3 || tx.calls!=3 {t.Fatal("multi success",got,err,tx.calls)}
   } else if err!=ErrConflict || got!=nil || tx.calls!=failAt {t.Fatal("partial selection escaped",got,err,tx.calls)}
   for i:=1;i<len(tx.ids);i++ {if tx.ids[i-1].String()>=tx.ids[i].String() {t.Fatal("unstable target lock order",tx.ids)}}
  })
 }
}
