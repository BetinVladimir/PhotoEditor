package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func TestMultiReplacementRegistrationRefusesBeforeTransaction(t *testing.T){
 prepared:=PreparedNegativeReplacementInputs{bucket:"photos"};db:=&negativeDraftStoreDB{}
 for _,bucket:=range []string{"foreign","photos"}{
  graph,err:=prepared.RegisterMultiReplacementInputs(context.Background(),db,bucket,nil,nil,nil,nil,MultiReplacementDraft{raw:"{}"},uuid.New(),LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{},v2input.Ref{})
  if err==nil||graph.ID!=uuid.Nil||db.begins!=0{t.Fatal("unprepared registration reached SQL",bucket,err)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=prepared.RegisterMultiReplacementInputs(ctx,db,"photos",nil,nil,nil,nil,MultiReplacementDraft{},uuid.New(),LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{},v2input.Ref{});err!=context.Canceled{t.Fatal("cancellation",err)}
 if db.begins!=0{t.Fatal("cancelled registration reached SQL")}
}
