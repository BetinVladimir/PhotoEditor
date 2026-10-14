package v2optical

import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
type negativeSubmitDouble struct { calls []string; fail string; queued bool; reads int }
func(d *negativeSubmitDouble) step(name string)error { d.calls=append(d.calls,name);if name==d.fail { return errors.New("ambiguous "+name) };return nil }
func(d *negativeSubmitDouble) PrepareParams(context.Context,auth.Identity,NegativePrepareRequest)(PreparedNegative,error) { return PreparedNegative{},d.step("prepare") }
func(d *negativeSubmitDouble) PublishTransport(context.Context,auth.Identity,NegativePrepareRequest,PreparedNegative)(v2input.GraphTask,error) { return v2input.GraphTask{},d.step("publish") }
func(d *negativeSubmitDouble) Admit(context.Context,auth.Identity,NegativePrepareRequest,PreparedNegative)error { d.queued=true;return d.step("admit") }
func(d *negativeSubmitDouble) ReadQueuedReceipt(_ context.Context,_ auth.Identity,r NegativePrepareRequest)(NegativeReceipt,error) {
 d.calls=append(d.calls,"receipt");d.reads++
 if len(r.Params)>0 { r.Params[0]='X' }
 if !d.queued { return NegativeReceipt{},v2input.ErrExpired }
 return NegativeReceipt{State:"queued"},nil
}
func TestNegativeSubmitRecoversBeforePublicationAndAfterAmbiguousAdmission(t *testing.T) {
 for _,mode:=range []string{"already_queued","success","ambiguous_admit","publication_failure"} {
  t.Run(mode,func(t *testing.T){
   d:=&negativeSubmitDouble{queued:mode=="already_queued"}
   if mode=="ambiguous_admit" { d.fail="admit" };if mode=="publication_failure" { d.fail="publish" }
   params:=[]byte(`{}`)
   got,err:=SubmitNegative(context.Background(),d,auth.Identity{},NegativePrepareRequest{Params:params})
   if mode=="publication_failure" { if err==nil { t.Fatal("publication error hidden") } } else if err!=nil || got.State!="queued" { t.Fatal("receipt not recovered",err) }
   if params[0]!='{' { t.Fatal("callback mutated caller request") }
   counts:=map[string]int{};for _,call:=range d.calls { counts[call]++ }
   if counts["admit"]>1 || mode=="already_queued"&&(counts["prepare"]!=0||counts["publish"]!=0||counts["admit"]!=0) { t.Fatal("replayed admitted request",d.calls) }
   if mode=="publication_failure"&&counts["admit"]!=0 { t.Fatal("activated failed publication") }
  })
 }
}
func TestNegativeSubmitCancelledBeforeAnyCallback(t *testing.T) {
 ctx,cancel:=context.WithCancel(context.Background());cancel();d:=&negativeSubmitDouble{}
 if _,err:=SubmitNegative(ctx,d,auth.Identity{},NegativePrepareRequest{});err!=context.Canceled || len(d.calls)!=0 { t.Fatal("cancelled call progressed",err,d.calls) }
}
