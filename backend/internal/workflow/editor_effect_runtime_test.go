package workflow
import("context";"errors";"testing";"photoeditor/backend/internal/v2render")
type editorCalibrationFixture struct{}
func(editorCalibrationFixture)Resolve(context.Context,v2render.RenderQualification)(v2render.RenderCalibration,error){return v2render.RenderCalibration{},errors.New("fixture does not qualify renders")}
func TestEditorCalibrationStartupFailClosed(t *testing.T){
 for _,env:=range []map[string]string{{},{"PHOTO_RENDER_CALIBRATION_FILE":"/operator/profile.json"},{"PHOTO_RENDER_CALIBRATION_SHA256":"pin"}}{
  called:=false;_,err:=loadEditorRenderCalibration(func(key string)string{return env[key]},func(string,string)(v2render.RenderCalibrationCatalog,error){called=true;return editorCalibrationFixture{},nil})
  if err==nil||called{t.Fatal("missing prerequisite reached loader")}
 }
 env:=func(key string)string{if key=="PHOTO_RENDER_CALIBRATION_FILE"{return "/operator/profile.json"};return "pin"}
 if _,err:=loadEditorRenderCalibration(env,func(string,string)(v2render.RenderCalibrationCatalog,error){return nil,errors.New("invalid pin")});err==nil{t.Fatal("invalid catalog accepted")}
 if _,err:=loadEditorRenderCalibration(env,func(string,string)(v2render.RenderCalibrationCatalog,error){return nil,nil});err==nil{t.Fatal("absent catalog accepted")}
 catalog:=editorCalibrationFixture{};result,err:=loadEditorRenderCalibration(env,func(path,pin string)(v2render.RenderCalibrationCatalog,error){if path!="/operator/profile.json"||pin!="pin"{t.Fatal("operator pins changed")};return catalog,nil})
 if err!=nil||result!=catalog{t.Fatal("verified loader result lost",err)}
}
