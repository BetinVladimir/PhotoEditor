package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2qualification"
)

func TestReplacementDraftStoreRejectsBeforeTransaction(t *testing.T){
 var prepared PreparedNegativeReplacementInputs
 db:=&negativeDraftStoreDB{}
 for _,draft:=range []NegativeReplacementDraft{{},{raw:"{}"}}{
  err:=prepared.SaveNegativeReplacementDraft(context.Background(),db,nil,nil,nil,nil,draft,uuid.New(),LocatedNegativeQualification{},v2qualification.DeploymentLocks{},NegativeResourceEstimate{})
  if err==nil||db.begins!=0{t.Fatal("invalid package opened transaction",err,db.begins)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=prepared.SaveNegativeReplacementDraft(ctx,db,nil,nil,nil,nil,NegativeReplacementDraft{},uuid.New(),LocatedNegativeQualification{},v2qualification.DeploymentLocks{},NegativeResourceEstimate{});err!=context.Canceled{t.Fatal("cancellation",err)}
 if db.begins!=0{t.Fatal("cancelled save opened transaction")}
}
