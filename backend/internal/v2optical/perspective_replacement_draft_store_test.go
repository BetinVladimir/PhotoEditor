package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2qualification"
)

func TestManualPerspectiveReplacementDraftStoreRejectsBeforeTransaction(t *testing.T){
 var prepared PreparedNegativeReplacementInputs
 db:=&negativeDraftStoreDB{}
 for _,draft:=range []PerspectiveReplacementDraft{{},{raw:"{}"}}{
  err:=prepared.SaveManualPerspectiveReplacementDraft(context.Background(),db,nil,nil,nil,nil,draft,uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},100,PerspectiveResourceEstimate{})
  if err==nil||db.begins!=0{t.Fatal("invalid package opened transaction",err,db.begins)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=prepared.SaveManualPerspectiveReplacementDraft(ctx,db,nil,nil,nil,nil,PerspectiveReplacementDraft{},uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},100,PerspectiveResourceEstimate{});err!=context.Canceled{t.Fatal("cancellation",err)}
 if db.begins!=0{t.Fatal("cancelled save opened transaction")}
}
