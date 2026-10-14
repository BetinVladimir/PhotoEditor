package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func TestManualPerspectiveReplacementStageRejectsBeforeTransaction(t *testing.T){
 prepared:=PreparedNegativeReplacementInputs{bucket:"photos"}
 db:=&negativeDraftStoreDB{}
 for _,bucket:=range []string{"foreign","photos"}{
  err:=prepared.StageManualPerspectiveReplacementTask(context.Background(),db,bucket,nil,nil,nil,nil,PerspectiveReplacementDraft{raw:"{}"},uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},100,PerspectiveResourceEstimate{},v2input.Ref{})
  if err==nil||db.begins!=0{t.Fatal("invalid staging reached SQL",bucket,err)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=prepared.StageManualPerspectiveReplacementTask(ctx,db,"photos",nil,nil,nil,nil,PerspectiveReplacementDraft{},uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},100,PerspectiveResourceEstimate{},v2input.Ref{});err!=context.Canceled{t.Fatal("cancellation",err)}
 if db.begins!=0{t.Fatal("cancelled staging reached SQL")}
}
