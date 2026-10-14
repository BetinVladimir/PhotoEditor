package interchange

import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/v2input"
)
func TestReviewTargetAuthorityRequiresCurrentBoundScopeBeforeIO(t *testing.T){
 authority:=SQLImportReviewTargetAuthority{}
 if err:=authority.QualifyImportBackgroundTarget(context.Background(),v2input.SessionVersionBinding{});err==nil{t.Fatal("accepted missing review binding")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=authority.QualifyImportBackgroundTarget(ctx,v2input.SessionVersionBinding{});!errors.Is(err,context.Canceled){t.Fatal("lost cancellation",err)}
}
