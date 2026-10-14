package interchange

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2draft"
)
func TestPreparedApprovalRejectsMismatchedScopeBeforeIO(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tid:=uuid.New();target:=ImportReviewTarget{Entry:uuid.New()}
 prepared:=SQLImportApprovalCatalog{Who:who,Scope:ImportPreparedScope{Tenant:tid,Entry:target.Entry}}
 for _,mode:=range []string{"tenant","entry","actor","session","cancelled"}{t.Run(mode,func(t *testing.T){
  candidate:=prepared;ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch mode{case "tenant":candidate.Scope.Tenant=uuid.New();case "entry":candidate.Scope.Entry=uuid.New();case "actor":candidate.Who.UserID=uuid.New();case "session":candidate.Who.SessionID=uuid.New();case "cancelled":cancel()}
  _,err:=PrepareImportApprovalWithPreparedCatalog(ctx,v2draft.Service{},who,tid,uuid.New(),target,candidate)
  if err==nil{t.Fatal("accepted invalid prepared approval scope")}
  if mode=="cancelled"&&!errors.Is(err,context.Canceled){t.Fatal("lost cancellation",err)}
 })}
}

func TestReviewApprovalRequiresInstalledStyleQualifierBeforeIO(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 _,err:=PrepareImportApprovalFromReview(context.Background(),v2draft.Service{},who,uuid.New(),uuid.New(),uuid.New(),uuid.New(),ImportReviewTarget{},1,"",nil,nil)
 if err==nil{t.Fatal("accepted missing full style qualifier")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 _,err=PrepareImportApprovalFromReview(ctx,v2draft.Service{},who,uuid.New(),uuid.New(),uuid.New(),uuid.New(),ImportReviewTarget{},1,"",nil,nil)
 if !errors.Is(err,context.Canceled){t.Fatal("lost cancellation",err)}
}
