package interchange

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type transportPublisher struct{calls int;badHash,collision bool;bundle uuid.UUID;task uuid.UUID}
func(p *transportPublisher)Publish(_ context.Context,task uuid.UUID,ordinal int,contract string,raw []byte)(v2input.Ref,error){
 p.calls++;p.task=task;sum:=sha256.Sum256(raw);hash:=hex.EncodeToString(sum[:]);if p.badHash{hash="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
 id:=uuid.NewSHA1(task,[]byte(contract));if p.collision{id=p.bundle};return v2input.Ref{ArtifactID:id,SHA256:hash},nil
}
func TestParseTransportOwnedPublicationAndPreflight(t *testing.T){
 tid,who,request,evidence,limits:=pendingFixture();plan,err:=CompileParsePlan(tid,who,request,"key",evidence,limits);if err!=nil{t.Fatal(err)}
 pub:=&transportPublisher{};transport,err:=plan.PublishInputs(context.Background(),pub);if err!=nil||pub.calls!=2||pub.task!=plan.Identity().Task{t.Fatal("publication failed",err)}
 task,err:=transport.DraftTask();if err!=nil||task.InputDigest==""||task.ID!=plan.Identity().Task||transport.Manifest().ArtifactID==uuid.Nil{t.Fatal("transport draft invalid",err)}
 for _,pub:=range []*transportPublisher{{badHash:true},{collision:true,bundle:evidence.Ref.ArtifactID}}{
  if _,err:=plan.PublishInputs(context.Background(),pub);err==nil{t.Fatal("invalid publisher receipt accepted")}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel();pub.calls=0
 if _,err:=plan.PublishInputs(ctx,pub);err!=context.Canceled||pub.calls!=0{t.Fatal("cancelled publication")}
 if err:=transport.RegisterInputs(context.Background(),nil,uuid.New(),plan.Identity().Run);err==nil{t.Fatal("foreign/nil registration")}
}
