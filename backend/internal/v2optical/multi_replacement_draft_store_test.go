package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
func TestMultiReplacementDraftStoreRejectsMissingAuthority(t *testing.T){
 var p PreparedNegativeReplacementInputs
 if err:=p.SaveMultiReplacementDraft(context.Background(),nil,nil,nil,nil,nil,MultiReplacementDraft{raw:"{}"},uuid.New(),LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{});err!=v2input.ErrInvalid{t.Fatal(err)}
 if _,err:=p.LoadMultiReplacementDraft(context.Background(),nil,nil,nil,nil,nil,MultiReplacementDraft{raw:"{}"},uuid.New(),LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{});err!=v2input.ErrInvalid{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=p.SaveMultiReplacementDraft(ctx,nil,nil,nil,nil,nil,MultiReplacementDraft{},uuid.Nil,LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{});err!=context.Canceled{t.Fatal(err)}
 if _,err:=p.LoadMultiReplacementDraft(ctx,nil,nil,nil,nil,nil,MultiReplacementDraft{},uuid.Nil,LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{});err!=context.Canceled{t.Fatal(err)}
}
