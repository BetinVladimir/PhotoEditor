package delivery

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
func TestWatermarkRecoveryPreflight(t *testing.T){
 intent,_,_,_:=createDocumentsFixture(t);scope,config,_:=watermarkPinFixture(t)
 who:=auth.Identity{UserID:intent.actor,SessionID:uuid.New()};reader:=&watermarkEvidenceReader{}
 service:=Service{}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,_,err:=service.RecoverWatermarkPin(ctx,who,intent.tenant,intent,scope.Preset,scope.PresetSHA,config,"photos",reader);err!=context.Canceled{t.Fatal(err)}
 for _,mode:=range []string{"tenant","actor","session","preset","bucket","database"}{t.Run(mode,func(t *testing.T){
  tid:=intent.tenant;identity:=who;preset:=scope.Preset;bucket:="photos"
  switch mode{case "tenant":tid=uuid.New();case "actor":identity.UserID=uuid.New();case "session":identity.SessionID=uuid.Nil;case "preset":preset=uuid.Nil;case "bucket":bucket="x"}
  if _,found,err:=service.RecoverWatermarkPin(context.Background(),identity,tid,intent,preset,scope.PresetSHA,config,bucket,reader);err==nil||found{t.Fatal("invalid recovery accepted",mode)}
 })}
 if reader.opens!=0{t.Fatal("object read before valid transaction")}
}
