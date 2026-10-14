package workflow

import (
 "context"
 "fmt"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func watermarkPresentationAlphaNPY(w,h int64)[]byte{
 header:=fmt.Sprintf("{'descr': '<f4', 'fortran_order': False, 'shape': (%d, %d), }",h,w)
 header+=strings.Repeat(" ",(16-(10+len(header)+1)%16)%16)+"\n"
 out:=append([]byte("\x93NUMPY\x01\x00"),byte(len(header)),byte(len(header)>>8));out=append(out,[]byte(header)...)
 return append(out,make([]byte,w*h*4)...)
}
func exerciseWatermarkPresentationLoader(t *testing.T,request delivery.FrozenExportRequest,pin delivery.FrozenWatermarkSource,scope delivery.WatermarkPinScope,source []byte,prefix []DeliveryPresentationOutput,result DeliveryPresentationOutput,registry Registry,limits DeliveryDerivativeLimits){
 t.Helper()
 image,err:=review.ParseWorkingImage(result.ImageJSON);if err!=nil{t.Fatal(err)}
 next,err:=CompileDeliveryWatermarkPrefix(request,pin,scope,source,prefix,registry,limits);if err!=nil{t.Fatal(err)}
 bodies:=deliveryStageObjects{"image":result.ImageJSON,"report":result.ReportJSON,"rgb":deliveryTestRGBNPY(image.Canvas.Width,image.Canvas.Height)}
 producer:=DeliveryStageResult{TaskID:next.Node.ID,ResultID:uuid.New(),Attempt:1,Fence:1}
 entries:=[]struct{key,role,media string;ref review.ArtifactRef}{{"image","working_image","application/json",result.ImageRef},{"report","derivative_report","application/json",result.ReportRef},{"rgb","working_rgb","application/octet-stream",image.Pixels}}
 if image.Alpha!=nil{bodies["alpha"]=watermarkPresentationAlphaNPY(image.Canvas.Width,image.Canvas.Height);entries=append(entries,struct{key,role,media string;ref review.ArtifactRef}{"alpha","working_alpha","application/octet-stream",*image.Alpha})}
 for _,entry:=range entries{body:=bodies[entry.key];producer.Outputs=append(producer.Outputs,DeliveryStageOutput{Ref:v2input.Ref{ArtifactID:entry.ref.ID,SHA256:entry.ref.SHA256},Role:entry.role,Bucket:"photos",Key:entry.key,Version:"v1",Bytes:int64(len(body)),MediaType:entry.media})}
 raw,err:=canonical.Marshal(producer);if err!=nil{t.Fatal(err)}
 lease:=delivery.ExpansionLease{Tenant:scope.Tenant,Run:scope.Parent,Expansion:uuid.New(),Token:uuid.New()}
 for _,mode:=range []string{"valid","pin_revoked","producer_changed","corrupt_rgb"}{
  objects:=deliveryStageObjects{};for key,body:=range bodies{objects[key]=append([]byte(nil),body...)}
  if mode=="corrupt_rgb"{objects["rgb"][len(objects["rgb"])-1]^=1}
  second:=raw;if mode=="producer_changed"{changed:=producer;changed.Fence++;second,_=canonical.Marshal(changed)}
  db:=&deliveryStageReadDB{responses:[][]byte{raw,second}}
  pins:=&watermarkLoaderPins{pin:pin,refuseSecond:mode=="pin_revoked"}
  loaded,err:=LoadVerifiedWatermarkPresentationStage(context.Background(),db,pins,objects,"photos",lease,request,scope,source,prefix,registry,limits)
  if mode!="valid"{if err==nil{t.Fatal("loader accepted",mode)};continue}
  if err!=nil||loaded==nil||db.calls!=2||pins.calls!=2{t.Fatal("physical presentation loader",err)}
  copy:=loaded.Output();copy.ImageJSON[0]='!';if loaded.Output().ImageJSON[0]!='{'{t.Fatal("borrowed evidence output")}
 }
}
