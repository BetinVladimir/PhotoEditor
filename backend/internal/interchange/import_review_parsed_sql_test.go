package interchange

import (
 "context"
 "errors"
 "testing"
)
func TestReviewParsedSQLSourceRejectsMissingScopeAndCancellation(t *testing.T){
 source:=SQLImportReviewParsedSource{}
 if _,_,err:=source.ResolveReviewParsed(context.Background());err==nil{t.Fatal("accepted missing current review authority")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,_,err:=source.ResolveReviewParsed(ctx);!errors.Is(err,context.Canceled){t.Fatal("lost cancellation",err)}
}
