package v2portrait

import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/v2input"
)

func TestKeyedRelightReceiptPrecedesNewDraftAndSubmission(t *testing.T) {
 receipt:=[]byte(`{"accepted":true}`)
 body,err:=submitKeyedRelight(context.Background(),func()([]byte,error){return receipt,nil},func()error{t.Fatal("created draft despite receipt");return nil},func()([]byte,error){t.Fatal("resubmitted accepted run");return nil,nil})
 if err!=nil || string(body)!=string(receipt){t.Fatal(string(body),err)}
 body[0]='x';if receipt[0]!='{' {t.Fatal("receipt alias escaped")}
}
func TestKeyedRelightOnlyExplicitReceiptAbsenceAllowsNewWork(t *testing.T) {
 for _,readErr:=range []error{v2input.ErrInvalid,context.DeadlineExceeded,errors.New("authority revoked")} {
  _,err:=submitKeyedRelight(context.Background(),func()([]byte,error){return nil,readErr},func()error{t.Fatal("read refusal created draft");return nil},func()([]byte,error){t.Fatal("read refusal admitted work");return nil,nil})
  if !errors.Is(err,readErr){t.Fatal(err)}
 }
}
func TestKeyedRelightAbsentReceiptCreatesThenSubmits(t *testing.T) {
 events:=[]string{}
 body,err:=submitKeyedRelight(context.Background(),func()([]byte,error){events=append(events,"read");return nil,v2input.ErrExpired},func()error{events=append(events,"create");return nil},func()([]byte,error){events=append(events,"submit");return []byte(`{"accepted":true}`),nil})
 if err!=nil || len(body)==0 || len(events)!=3 || events[0]!="read" || events[1]!="create" || events[2]!="submit" {t.Fatal(events,err)}
}
func TestKeyedRelightRecoversLostAcknowledgementWithoutSecondMutation(t *testing.T) {
 for _,stage:=range []string{"create","submit"} {t.Run(stage,func(t *testing.T){
  reads,creates,submits:=0,0,0
  body,err:=submitKeyedRelight(context.Background(),func()([]byte,error){reads++;if reads==1{return nil,v2input.ErrExpired};return []byte(`{"accepted":true}`),nil},func()error{creates++;if stage=="create"{return errors.New("lost draft acknowledgement")};return nil},func()([]byte,error){submits++;return nil,errors.New("lost admission acknowledgement")})
  expectedSubmits:=1;if stage=="create" {expectedSubmits=0}
  if err!=nil || len(body)==0 || reads!=2 || creates!=1 || submits!=expectedSubmits {t.Fatal(reads,creates,submits,err)}
 })}
}
func TestKeyedRelightKeepsOriginalAndRecoveryErrors(t *testing.T) {
 original,recovery:=errors.New("admission failed"),errors.New("authority lost")
 reads:=0
 _,err:=submitKeyedRelight(context.Background(),func()([]byte,error){reads++;if reads==1{return nil,v2input.ErrExpired};return nil,recovery},func()error{return original},func()([]byte,error){t.Fatal("unexpected submit");return nil,nil})
 if !errors.Is(err,original)||!errors.Is(err,recovery){t.Fatal(err)}
}
func TestKeyedRelightCancellationStopsFreshAdmission(t *testing.T) {
 ctx,cancel:=context.WithCancel(context.Background())
 _,err:=submitKeyedRelight(ctx,func()([]byte,error){cancel();return nil,v2input.ErrExpired},func()error{t.Fatal("created after cancellation");return nil},func()([]byte,error){t.Fatal("submitted after cancellation");return nil,nil})
 if !errors.Is(err,context.Canceled){t.Fatal(err)}
}

func TestKeyedRelightReplayOnlyNeverCreatesAbsentRequest(t *testing.T) {
 ctx:=WithRelightReplayOnly(context.Background())
 reads:=0
 _,err:=submitKeyedRelight(ctx,func()([]byte,error){reads++;return nil,v2input.ErrExpired},func()error{t.Fatal("replay created draft");return nil},func()([]byte,error){t.Fatal("replay admitted task");return nil,nil})
 if !errors.Is(err,v2input.ErrExpired)||reads!=1 {t.Fatal(reads,err)}
}
func TestKeyedRelightReplayOnlyReturnsHistoricalReceipt(t *testing.T) {
 ctx:=WithRelightReplayOnly(context.Background())
 body,err:=submitKeyedRelight(ctx,func()([]byte,error){return []byte(`{"accepted":true}`),nil},func()error{t.Fatal("replay created draft");return nil},func()([]byte,error){t.Fatal("replay submitted");return nil,nil})
 if err!=nil||string(body)!=`{"accepted":true}` {t.Fatal(string(body),err)}
}
