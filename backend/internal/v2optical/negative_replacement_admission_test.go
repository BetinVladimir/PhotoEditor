package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func TestReplacementAdmissionRefusesBeforeTransaction(t *testing.T){
 prepared:=PreparedNegativeReplacementInputs{bucket:"photos"};db:=&negativeDraftStoreDB{}
 for _,bucket:=range []string{"foreign","photos"}{
  graph,err:=prepared.AdmitNegativeReplacementGraph(context.Background(),db,bucket,nil,nil,nil,nil,NegativeReplacementDraft{raw:"{}"},uuid.New(),LocatedNegativeQualification{},v2qualification.DeploymentLocks{},NegativeResourceEstimate{},v2input.Ref{})
  if err==nil||graph.ID!=uuid.Nil||db.begins!=0{t.Fatal("unprepared admission reached SQL",bucket,err)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=prepared.AdmitNegativeReplacementGraph(ctx,db,"photos",nil,nil,nil,nil,NegativeReplacementDraft{},uuid.New(),LocatedNegativeQualification{},v2qualification.DeploymentLocks{},NegativeResourceEstimate{},v2input.Ref{});err!=context.Canceled{t.Fatal("cancellation",err)}
 if db.begins!=0{t.Fatal("cancelled admission reached SQL")}
}
