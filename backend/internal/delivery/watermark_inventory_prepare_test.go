package delivery

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
type watermarkInventoryService struct{calls int;prepare func(uuid.UUID,string,[]byte)(FrozenWatermarkSource,error)}
func(s *watermarkInventoryService)PrepareWatermarkPin(_ context.Context,_ auth.Identity,_ uuid.UUID,_ CreateIntent,id uuid.UUID,sha string,config []byte,_ string,_ v2input.SessionAnalysisObjectReader)(FrozenWatermarkSource,error){s.calls++;return s.prepare(id,sha,config)}
func TestWatermarkInventoryPreflightAndPartialFailure(t *testing.T){
 intent,_,_,_:=createDocumentsFixture(t);_,config,e:=watermarkPinFixture(t)
 first,second:=uuid.New(),uuid.New();intent.request.Policy.RequiredPresets=[]uuid.UUID{first,second};intent.request.Policy.OptionalPresets=[]uuid.UUID{}
 who:=auth.Identity{UserID:intent.actor,SessionID:uuid.New()}
 presets:=[]CreateCatalogPreset{{ID:first,SHA256:digest(config),ConfigJCS:config},{ID:second,SHA256:digest(config),ConfigJCS:config}}
 reader:=&watermarkEvidenceReader{}
 for _,mode:=range []string{"complete","invalid_second","failure_second","foreign_pin"}{t.Run(mode,func(t *testing.T){
  inputs:=append([]CreateCatalogPreset(nil),presets...)
  if mode=="invalid_second"{inputs[1].SHA256="invalid"}
  service:=&watermarkInventoryService{}
  service.prepare=func(id uuid.UUID,sha string,raw []byte)(FrozenWatermarkSource,error){
   if mode=="failure_second"&&id==second{return FrozenWatermarkSource{},errors.New("publication unavailable")}
   scope:=WatermarkPinScope{Tenant:intent.tenant,Project:intent.request.ProjectID,Actor:intent.actor,Parent:intent.identity.Run,Preset:id,PresetSHA:sha}
   if mode=="foreign_pin"{scope.Parent=uuid.New()}
   return CompileWatermarkSourcePin(scope,raw,e)
  }
  out,err:=PrepareWatermarkInventory(context.Background(),service,who,intent.tenant,intent,inputs,"photos",reader)
  if mode=="complete"{if err!=nil||len(out)!=2||service.calls!=2{t.Fatal("complete",err)};return}
  if err==nil||out!=nil{t.Fatal("partial inventory escaped",mode)}
  if mode=="invalid_second"&&service.calls!=0{t.Fatal("effects before full preflight")}
 })}
}
