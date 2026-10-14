package workflow

import (
 "context"
 "testing"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)
func TestAutomaticPerspectiveParamsRejectsMissingDraftBeforeDB(t *testing.T){
 db:=&detectionDocumentDB{}
 repo:=automaticPerspectiveParamsRepository{source:ReplacementDetectionDocuments{DB:db}}
 if _,err:=repo.Reserve(context.Background(),artifactjson.Scope{},"",2);err==nil{t.Fatal("empty draft reserved")}
 if _,err:=repo.Complete(context.Background(),artifactjson.Scope{},artifactjson.Reservation{},objectstore.Object{});err==nil{t.Fatal("empty draft completed")}
 if db.calls!=0{t.Fatal("invalid packet reached database")}
 if _,err:=repo.source.PublishAutomaticPerspectiveParams(context.Background(),nil,ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err==nil{t.Fatal("missing object store accepted")}
}
func TestAutomaticPerspectiveStageRejectsMissingAndCancelledDraft(t *testing.T){
 r:=ReplacementDetectionDocuments{}
 if err:=r.StageAutomaticPerspectiveTask(context.Background(),v2optical.PerspectiveReplacementDraft{},ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err==nil{t.Fatal("missing draft staged")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=r.StageAutomaticPerspectiveTask(ctx,v2optical.PerspectiveReplacementDraft{},ReadyReplacementSegments{},v2optical.LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,v2optical.PerspectiveResourceEstimate{});err!=context.Canceled{t.Fatal(err)}
}
