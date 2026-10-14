package interchange

import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestImportStyleScopeBindsCurrentMasterICCAndCanvas(t *testing.T){
 _,master,masterRaw:=importWorkingValidatorFixture(t)
 image,err:=review.ParseWorkingImage(masterRaw);if err!=nil{t.Fatal(err)}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
 raw,err:=os.ReadFile("../run/identity_style.json");if err!=nil{t.Fatal(err)}
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:master.Ref,CanvasHash:canvas}
 for _,mode:=range []string{"valid","icc","canvas","master_hash","cancelled"}{t.Run(mode,func(t *testing.T){
  reader:=&importMasterReaderStub{body:masterRaw};authority:=&backgroundPublishAuthority{}
  scope:=BoundImportStyleScope{WorkICCSHA:image.Color.ICCSHA256,Masters:reader,Targets:authority}
  target:=binding
  switch mode{case "icc":scope.WorkICCSHA=rawSHA([]byte("other ICC"));case "canvas":target.CanvasHash=rawSHA([]byte("other canvas"));case "master_hash":reader.body=[]byte(`{}`)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err:=scope.QualifyTargetStyleScope(ctx,raw,target)
  if mode=="valid"{if err!=nil||authority.calls!=1{t.Fatal("valid current style scope refused",err)};return}
  if err==nil{t.Fatal("accepted invalid style scope")}
 })}
}
