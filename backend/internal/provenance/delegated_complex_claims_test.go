package provenance

import (
 "context"
 "errors"
 "testing"
)

func TestDelegatedComplexClaimsRequireScopedReaders(t *testing.T){
 ready:=ReadySigningExport{}
 render:=DelegatedRenderSigningTaskResolver{}
 negative:=DelegatedNegativeSigningTaskResolver{}
 if _,err:=render.ResolveDelegatedTask(context.Background(),ready,
  CanonicalSigningTask{Operation:"render.compose",Version:"1"});err==nil{
  t.Fatal("render claims without evidence accepted")
 }
 if _,err:=negative.ResolveDelegatedTask(context.Background(),ready,
  CanonicalSigningTask{Operation:"optical.negative",Version:"1"});err==nil{
  t.Fatal("negative claims without evidence accepted")
 }
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,err:=render.ResolveDelegatedTask(ctx,ready,CanonicalSigningTask{});
  !errors.Is(err,context.Canceled){t.Fatal("render cancellation lost",err)}
 if _,err:=negative.ResolveDelegatedTask(ctx,ready,CanonicalSigningTask{});
  !errors.Is(err,context.Canceled){t.Fatal("negative cancellation lost",err)}
}
