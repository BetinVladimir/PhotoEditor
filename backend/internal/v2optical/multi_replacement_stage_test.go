package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func TestMultiReplacementStageRejectsBeforeTransaction(t *testing.T){
 prepared:=PreparedNegativeReplacementInputs{bucket:"photos"}
 db:=&negativeDraftStoreDB{}
 for _,bucket:=range []string{"foreign","photos"}{
  err:=prepared.StageMultiReplacementTask(context.Background(),db,bucket,nil,nil,nil,nil,MultiReplacementDraft{raw:"{}"},uuid.New(),LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{},v2input.Ref{})
  if err==nil||db.begins!=0{t.Fatal("invalid staging reached SQL",bucket,err)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=prepared.StageMultiReplacementTask(ctx,db,"photos",nil,nil,nil,nil,MultiReplacementDraft{},uuid.New(),LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{},v2input.Ref{});err!=context.Canceled{t.Fatal("cancellation",err)}
 if db.begins!=0{t.Fatal("cancelled staging reached SQL")}
}
