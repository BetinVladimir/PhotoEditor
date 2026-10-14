package delivery

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
func TestWatermarkPreparationRejectsBeforeObjectIO(t *testing.T){
 intent,_,_,_:=createDocumentsFixture(t);scope,config,_:=watermarkPinFixture(t)
 who:=auth.Identity{UserID:intent.actor,SessionID:uuid.New()};reader:=&watermarkEvidenceReader{}
 service:=Service{}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=service.PrepareWatermarkPin(ctx,who,intent.tenant,intent,scope.Preset,scope.PresetSHA,config,"photos",reader);err!=context.Canceled{t.Fatal(err)}
 if _,err:=service.PrepareWatermarkPin(context.Background(),who,uuid.New(),intent,scope.Preset,scope.PresetSHA,config,"photos",reader);err==nil{t.Fatal("foreign tenant accepted")}
 if _,err:=service.PrepareWatermarkPin(context.Background(),who,intent.tenant,intent,scope.Preset,scope.PresetSHA,config,"photos",reader);err==nil{t.Fatal("missing database accepted")}
 if reader.opens!=0{t.Fatal("object IO before selection commit")}
}
