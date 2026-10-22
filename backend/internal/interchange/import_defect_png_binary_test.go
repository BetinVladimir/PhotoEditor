package interchange

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestImportDefectPNGRequiresOccurrencePublisherAndEntryScope(t *testing.T){
 for _,mode:=range []string{"missing_publisher","wrong_contract","cancelled"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  kind:="DefectLabelPNG";if mode=="wrong_contract"{kind="EncodedImage"}
  result,size,err:=(ImportDefectPNGBinaryRemapper{}).RemapImportBinary(ctx,uuid.New(),v2input.Ref{},v2input.Ref{},kind,v2input.SessionVersionBinding{},v2input.SessionVersionBinding{},1<<20)
  if err==nil||result!=(v2input.Ref{})||size!=0{t.Fatal("uninstalled copy accepted")}
  if mode=="cancelled"&&!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
 })}
}
func TestImportDefectPNGRouteRequiresDedicatedCodec(t *testing.T){
 codec:=&binaryRouteStub{kind:"DefectLabelPNG"}
 router:=ImportBinaryCodecRouter{DefectPNGs:codec}
 _,_,err:=router.RemapImportBinary(context.Background(),uuid.New(),v2input.Ref{},v2input.Ref{},"DefectLabelPNG",v2input.SessionVersionBinding{},v2input.SessionVersionBinding{},100)
 if err!=nil{t.Fatal(err)}
 _,_,err=(ImportBinaryCodecRouter{DefectLabels:codec}).RemapImportBinary(context.Background(),uuid.New(),v2input.Ref{},v2input.Ref{},"DefectLabelPNG",v2input.SessionVersionBinding{},v2input.SessionVersionBinding{},100)
 if err==nil{t.Fatal("NPY label codec substituted for PNG")}
}
