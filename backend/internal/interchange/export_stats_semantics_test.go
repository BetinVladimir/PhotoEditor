package interchange
import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2input"
)
type statsAuthorityStub struct {calls int;deny bool}
func(s *statsAuthorityStub)QualifyExportStats(_ context.Context,_ v2input.Entry,raw []byte)error {
 s.calls++;raw[0]='!';if s.deny{return v2input.ErrInvalid};return nil
}
func TestStatsSemanticsRequiresProducerAndBasePolicies(t *testing.T){
 for _,mode:=range []string{"valid","producer-deny","base-deny","missing-producer","missing-base","cancelled","ordinary"}{t.Run(mode,func(t *testing.T){
  producer:=&statsAuthorityStub{deny:mode=="producer-deny"};base:=&exportSemanticsStub{deny:mode=="base-deny"}
  policy:=ExportStatsSemantics{Authority:producer,Other:base}
  if mode=="missing-producer"{policy.Authority=nil};if mode=="missing-base"{policy.Other=nil}
  raw:=[]byte("{}");entry:=v2input.Entry{Contract:"Stats20"};if mode=="ordinary"{entry.Contract="MaskRef"}
  ctx:=context.Background();if mode=="cancelled"{var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  err:=policy.QualifyExportDependency(ctx,entry,raw,nil)
  if mode=="valid"||mode=="ordinary"{if err!=nil||base.calls!=1{t.Fatal("base policy missing",err)}}else if err==nil{t.Fatal("unqualified stats accepted")}
  if mode=="producer-deny"||mode=="missing-producer"||mode=="missing-base"||mode=="cancelled"{if base.calls!=0{t.Fatal("failed producer reached base")}}
  if mode=="ordinary"&&producer.calls!=0{t.Fatal("ordinary dependency passed stats producer")}
  if mode=="producer-deny"&&string(raw)!="{}"{t.Fatal("producer mutated caller bytes")}
 })}
}
