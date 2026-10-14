package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2qualification"
)

func TestReplacementDraftLockRefusesAbsentTransaction(t *testing.T){
 var prepared PreparedNegativeReplacementInputs
 got,err:=prepared.LockNegativeReplacementDraft(context.Background(),nil,nil,nil,nil,nil,[]byte("{}"),uuid.New(),LocatedNegativeQualification{},v2qualification.DeploymentLocks{},NegativeResourceEstimate{})
 if err==nil||len(got.Bytes())!=0{t.Fatal("missing transaction accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=prepared.LockNegativeReplacementDraft(ctx,nil,nil,nil,nil,nil,nil,uuid.New(),LocatedNegativeQualification{},v2qualification.DeploymentLocks{},NegativeResourceEstimate{});err!=context.Canceled{t.Fatal("cancellation",err)}
}
