package delivery

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)

func TestWatermarkReauthorizationPreflightBeforeIO(t *testing.T){
 scope,config,e:=watermarkPinFixture(t);pin,err:=CompileWatermarkSourcePin(scope,config,e);if err!=nil{t.Fatal(err)}
 who:=auth.Identity{UserID:scope.Actor,SessionID:uuid.New()}
 reader:=&watermarkEvidenceReader{}
 service:=Service{}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=service.ReauthorizeWatermarkPin(ctx,who,scope.Tenant,scope,config,pin,"photos",reader);err!=context.Canceled{t.Fatal(err)}
 for _,mode:=range []string{"tenant","actor","session","pin","config","objects"}{t.Run(mode,func(t *testing.T){
  tid:=scope.Tenant;identity:=who;current:=pin;raw:=config
  switch mode{case "tenant":tid=uuid.New();case "actor":identity.UserID=uuid.New();case "session":identity.SessionID=uuid.Nil;case "pin":current=FrozenWatermarkSource{};case "config":raw=[]byte("{}");case "objects":
   if _,err:=service.ReauthorizeWatermarkPin(context.Background(),identity,tid,scope,raw,current,"photos",nil);err==nil{t.Fatal("nil reader accepted")};return
  }
  if _,err:=service.ReauthorizeWatermarkPin(context.Background(),identity,tid,scope,raw,current,"photos",reader);err==nil{t.Fatal("invalid authority accepted",mode)}
 })}
 if reader.opens!=0{t.Fatal("object IO during failed preflight")}
}
