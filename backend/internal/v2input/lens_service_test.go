package v2input

import (
 "context"
 "errors"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)

type lensBatchAdmissionFunc func(context.Context,LensAdmissionRequest)(JobTicket,error)
func(f lensBatchAdmissionFunc)Admit(ctx context.Context,r LensAdmissionRequest)(JobTicket,error){return f(ctx,r)}

func TestLensServiceBindsFrozenBatchAndLateCancellation(t *testing.T){
 tenant,project,actor,session,snapshot,version,profile:=uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New()
 who:=auth.Identity{UserID:actor,SessionID:session}
 intent:=LensIntent{1,snapshot,[]LensIntentChoice{{version,Ref{profile,strings.Repeat("a",64)},.75}}}
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 calls:=0
 service:=LensService{PolicySHA:strings.Repeat("b",64),Limits:LensAdmissionLimits{100,200,300}}
 service.Coordinator=lensBatchAdmissionFunc(func(_ context.Context,r LensAdmissionRequest)(JobTicket,error){
  calls++
  if r.Tenant!=tenant || r.Project!=project || r.Actor!=actor || r.Session!=session || r.Snapshot!=snapshot || r.PolicySHA!=service.PolicySHA || r.Limits!=service.Limits || len(r.Choices)!=1 || r.Choices[0].AssetVersionID!=version || r.Choices[0].ProfileJSON!=nil {t.Fatal("authority changed")}
  run,_:=RunID(tenant,actor,r.Key);workflow,_:=WorkflowID(run)
  if calls==2 {cancel()}
  return JobTicket{RunID:run,WorkflowID:workflow,State:"queued"},nil
 })
 if _,err:=service.SubmitProjectIntent(ctx,who,tenant,project,"lens-batch",intent);err!=nil {t.Fatal(err)}
 if ticket,err:=service.SubmitProjectIntent(ctx,who,tenant,project,"lens-batch",intent);!errors.Is(err,context.Canceled)||ticket.RunID!=uuid.Nil {t.Fatal("late success exposed",ticket,err)}
 if _,err:=service.SubmitProjectIntent(ctx,who,tenant,project,"lens-batch",intent);!errors.Is(err,context.Canceled)||calls!=2 {t.Fatal("cancelled call dispatched")}
}
