package v2optical

import (
 "context"
 "errors"
 "reflect"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)

type replacementSchedulerDouble struct{calls []string;fail string;who auth.Identity;deferred bool;decision NegativeSchedulerDecision}
func(f *replacementSchedulerDouble)Next(context.Context,uuid.UUID)(*NegativeSchedulerDecision,error){return &f.decision,nil}
func(f *replacementSchedulerDouble)Reconcile(context.Context,NegativeSchedulerDecision)error{f.calls=append(f.calls,"reconcile");if f.fail=="reconcile"{return errors.New("cancel unavailable")};return nil}
func(f *replacementSchedulerDouble)ResolveNegativeReplacementIdentity(context.Context,NegativeSchedulerDecision)(auth.Identity,error){f.calls=append(f.calls,"identity");if f.fail=="identity"{return auth.Identity{},errors.New("session unavailable")};return f.who,nil}
func(f *replacementSchedulerDouble)AdvanceProjectReplacementWave(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,d NegativeDeclinedDecision,limit int)(NegativeReplacementWavePass,error){
 f.calls=append(f.calls,"wave")
 if who!=f.who||tenant!=f.decision.Tenant||project!=f.decision.Project||d!=f.decision.Decision||limit!=1{return NegativeReplacementWavePass{},errors.New("scope changed")}
 if f.fail=="wave"{return NegativeReplacementWavePass{},errors.New("publication unavailable")}
 out:=NegativeReplacementWavePass{};if f.deferred{out.Deferred=[]NegativeReplacementNode{{}}};return out,nil
}
func TestReplacementSchedulerRetainsCancellationAndLiveIdentity(t *testing.T){
 for _,mode:=range []string{"valid","skip","reconcile","identity","empty_identity","wave","deferred"}{
  f:=&replacementSchedulerDouble{calls:[]string{},who:auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},fail:mode,deferred:mode=="deferred",decision:NegativeSchedulerDecision{Tenant:uuid.New(),Project:uuid.New(),Decision:NegativeDeclinedDecision{Route:"base",Review:uuid.New()}}}
  if mode=="skip"{f.decision.Decision.Route="skip"};if mode=="empty_identity"{f.who=auth.Identity{}}
  scheduler:=NegativeReplacementScheduler{Base:f,Projects:f,Identities:f,Limit:1}
  err:=scheduler.Reconcile(context.Background(),f.decision)
  want:=[]string{"reconcile","identity","wave"}
  if mode=="skip"||mode=="reconcile"{want=want[:1]}else if mode=="identity"||mode=="empty_identity"{want=want[:2]}
  if !reflect.DeepEqual(f.calls,want){t.Fatal("wrong scheduler order",mode,f.calls)}
  if mode=="valid"||mode=="skip"{if err!=nil{t.Fatal("valid scheduler pass refused",mode,err)}}else if err==nil{t.Fatal("failed scheduler pass accepted",mode)}
 }
}
