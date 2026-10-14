package v2optical

import (
 "bytes"
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2qualification"
)
func TestReplacementNegativeDraftRefusesMissingPreparation(t *testing.T){
 var step PreparedMappedReplacementStep
 for _,snapshot:=range []uuid.UUID{uuid.Nil,uuid.New()}{
  got,err:=step.CompileNegativeReplacementDraft(context.Background(),snapshot,NegativeQualificationPin{},v2qualification.DeploymentLocks{},nil,nil,NegativeResourceEstimate{})
  if err==nil||len(got.Bytes())!=0{t.Fatal("unprepared draft accepted")}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=step.CompileNegativeReplacementDraft(ctx,uuid.New(),NegativeQualificationPin{},v2qualification.DeploymentLocks{},nil,nil,NegativeResourceEstimate{});err!=context.Canceled{t.Fatal("cancellation",err)}
 owned:=NegativeReplacementDraft{raw:"{}"};raw:=owned.Bytes();raw[0]='[';if owned.Bytes()[0]=='['{t.Fatal("mutable draft bytes")}
}

func TestRestoreReplacementNegativeDraftRefusesUntrustedPackets(t *testing.T){
 var step PreparedMappedReplacementStep
 snapshot:=uuid.New()
 packets:=[][]byte{nil,[]byte("{}"),[]byte(`{"schema_version":1}`),[]byte(`{"schema_version":2,"unknown":true}`),[]byte("null"),bytes.Repeat([]byte(" "),(3<<20)+1)}
 for i,raw:=range packets{
  got,err:=step.RestoreNegativeReplacementDraft(context.Background(),raw,snapshot,NegativeQualificationPin{},v2qualification.DeploymentLocks{},nil,nil,NegativeResourceEstimate{})
  if err==nil||len(got.Bytes())!=0{t.Fatalf("packet %d accepted",i)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=step.RestoreNegativeReplacementDraft(ctx,nil,snapshot,NegativeQualificationPin{},v2qualification.DeploymentLocks{},nil,nil,NegativeResourceEstimate{});err!=context.Canceled{t.Fatal("cancellation",err)}
}
