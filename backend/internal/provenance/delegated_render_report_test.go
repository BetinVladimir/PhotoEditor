package provenance

import (
 "context"
 "errors"
 "testing"

 "photoeditor/backend/internal/v2input"
)

func TestDelegatedRenderReportRequiresBindingAndCancellation(t *testing.T){
 if _,err:=ReadDelegatedRenderReport(context.Background(),nil,
  CanonicalSigningTask{},v2input.SessionVersionBinding{},v2input.Ref{},nil);err==nil{
  t.Fatal("unbound report accepted")
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=ReadDelegatedRenderReport(ctx,nil,CanonicalSigningTask{},
  v2input.SessionVersionBinding{},v2input.Ref{},nil);
  !errors.Is(err,context.Canceled){t.Fatal("cancellation ignored",err)}
}
