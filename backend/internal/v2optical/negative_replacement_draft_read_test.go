package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2qualification"
)

func TestReplacementDraftReadRejectsBeforeTransaction(t *testing.T){
 var prepared PreparedNegativeReplacementInputs
 db:=&negativeDraftStoreDB{}
 for _,ids:=range [][2]uuid.UUID{{uuid.Nil,uuid.New()},{uuid.New(),uuid.Nil},{uuid.New(),uuid.New()}}{
  got,err:=prepared.LoadNegativeReplacementDraft(context.Background(),db,nil,nil,nil,nil,ids[0],ids[1],LocatedNegativeQualification{},v2qualification.DeploymentLocks{},NegativeResourceEstimate{})
  if err==nil||len(got.Bytes())!=0||db.begins!=0{t.Fatal("invalid recovery opened transaction",err,db.begins)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=prepared.LoadNegativeReplacementDraft(ctx,db,nil,nil,nil,nil,uuid.New(),uuid.New(),LocatedNegativeQualification{},v2qualification.DeploymentLocks{},NegativeResourceEstimate{});err!=context.Canceled{t.Fatal("cancellation",err)}
 if db.begins!=0{t.Fatal("cancelled recovery opened transaction")}
}
