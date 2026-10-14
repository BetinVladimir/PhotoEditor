package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func TestManualPerspectiveReplacementAdmissionRefusesBeforeTransaction(t *testing.T){
 prepared:=PreparedNegativeReplacementInputs{bucket:"photos"};db:=&negativeDraftStoreDB{}
 for _,bucket:=range []string{"foreign","photos"}{
  graph,err:=prepared.AdmitManualPerspectiveReplacementGraph(context.Background(),db,bucket,nil,nil,nil,nil,PerspectiveReplacementDraft{raw:"{}"},uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},100,PerspectiveResourceEstimate{},v2input.Ref{})
  if err==nil||graph.ID!=uuid.Nil||db.begins!=0{t.Fatal("unprepared admission reached SQL",bucket,err)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=prepared.AdmitManualPerspectiveReplacementGraph(ctx,db,"photos",nil,nil,nil,nil,PerspectiveReplacementDraft{},uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},100,PerspectiveResourceEstimate{},v2input.Ref{});err!=context.Canceled{t.Fatal("cancellation",err)}
 if db.begins!=0{t.Fatal("cancelled admission reached SQL")}
}
