package v2optical

import (
 "bytes"
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func replacementBinding(before v2input.SessionVersionBinding)OpticalReplacementBinding{
 after:=before;after.Source.ArtifactID=uuid.New();after.Source.SHA256="dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd";after.CanvasHash="eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
 return OpticalReplacementBinding{Before:before,After:after}
}
func TestOpticalRebindPreservesFrozenControlsAndFrameOrder(t *testing.T){
 negative,_,_,_:=negativeDraftFixture(t);p:=perspectiveIntent();frames:=multiFrames(3)
 panorama:=PanoramaParams{SchemaVersion:1,Frames:frames,ReferenceOrdinal:2,MaxPixels:123456,Calibration:multiCalibration(),Projection:"cylindrical"}
 focus:=FocusStackParams{SchemaVersion:1,Frames:frames,ReferenceOrdinal:1,MaxPixels:123456,Calibration:multiCalibration(),MotionPolicy:"reject"}
 for _,op:=range []string{"optical.negative","optical.perspective","optical.panorama","optical.focus_stack"}{
  t.Run(op,func(t *testing.T){var raw []byte;var binding OpticalReplacementBinding
   switch op {case "optical.negative":raw,_,_=negative.Compile();binding=replacementBinding(negative.Base)
   case "optical.perspective":raw,_,_=p.Compile();binding=replacementBinding(p.Base)
   case "optical.panorama":raw,_,_=panorama.Compile();binding=replacementBinding(frames[1])
   case "optical.focus_stack":raw,_,_=focus.Compile();binding=replacementBinding(frames[1])}
   saved:=append([]byte(nil),raw...)
   got,err:=RebindOpticalIntent(context.Background(),op,raw,[]OpticalReplacementBinding{binding},nil)
   if err!=nil||got.Operation()!=op||len(got.SHA256())!=64||!bytes.Equal(raw,saved) {t.Fatal("rebind",err)}
   switch op {
   case "optical.negative":after,err:=ParseNegativeParams(got.Params());want:=negative;want.Base=binding.After;if err!=nil||after!=want {t.Fatal("negative controls changed",err)}
   case "optical.perspective":after,err:=ParsePerspectiveParams(got.Params());after.Base=p.Base;restored,_,_:=after.Compile();if err!=nil||!bytes.Equal(restored,saved) {t.Fatal("perspective controls changed",err)}
   case "optical.panorama":after,err:=ParsePanoramaParams(got.Params());if err!=nil||after.Frames[1]!=binding.After {t.Fatal("frame replacement",err)};after.Frames[1]=frames[1];restored,_,_:=after.Compile();if !bytes.Equal(restored,saved) {t.Fatal("panorama order/controls changed")}
   case "optical.focus_stack":after,err:=ParseFocusStackParams(got.Params());if err!=nil||after.Frames[1]!=binding.After {t.Fatal("frame replacement",err)};after.Frames[1]=frames[1];restored,_,_:=after.Compile();if !bytes.Equal(restored,saved) {t.Fatal("focus order/controls changed")}
   }
   owned:=got.Params();owned[0]='[';if got.Params()[0]=='[' {t.Fatal("mutable intent")}
  })
 }
}
func TestOpticalRebindRejectsStaleAutomaticLinesAndUnusedBindings(t *testing.T){
 p:=perspectiveIntent();p.Mode="auto";p.Corners=nil;old:=multiCalibration();p.Lines=&old;raw,_,err:=p.Compile();if err!=nil {t.Fatal(err)}
 binding:=replacementBinding(p.Base)
 for _,lines:=range []*v2input.Ref{nil,&old} {if _,err:=RebindOpticalIntent(context.Background(),"optical.perspective",raw,[]OpticalReplacementBinding{binding},lines);err==nil {t.Fatal("stale lines accepted")}}
 fresh:=multiCalibration();got,err:=RebindOpticalIntent(context.Background(),"optical.perspective",raw,[]OpticalReplacementBinding{binding},&fresh);if err!=nil {t.Fatal(err)}
 parsed,err:=ParsePerspectiveParams(got.Params());if err!=nil||parsed.Lines==nil||*parsed.Lines!=fresh||parsed.Base!=binding.After {t.Fatal("new line binding",err)}
 for _,bindings:=range [][]OpticalReplacementBinding{nil,{binding,binding},{replacementBinding(multiFrames(1)[0])}} {if _,err:=RebindOpticalIntent(context.Background(),"optical.perspective",raw,bindings,&fresh);err==nil {t.Fatal("bad replacements accepted")}}
 binding.After.AssetVersionID=uuid.New()
 if _,err:=RebindOpticalIntent(context.Background(),"optical.perspective",raw,[]OpticalReplacementBinding{binding},&fresh);err==nil {t.Fatal("asset identity changed")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=RebindOpticalIntent(ctx,"optical.perspective",raw,nil,nil);err!=context.Canceled {t.Fatal("cancel ignored",err)}
}
