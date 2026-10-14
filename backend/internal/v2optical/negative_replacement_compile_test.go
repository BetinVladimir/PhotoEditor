package v2optical

import (
 "bytes"
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementNegativeRequalifiesNewParamsAndRefusesOldProof(t *testing.T){
 params,source,old,capacity:=negativeDraftFixture(t);raw,_,err:=params.Compile();if err!=nil {t.Fatal(err)}
 after:=params.Base;after.Source.ArtifactID=uuid.New()
 intent,err:=RebindOpticalIntent(context.Background(),"optical.negative",raw,[]OpticalReplacementBinding{{Before:params.Base,After:after}},nil);if err!=nil {t.Fatal(err)}
 if _,err:=CompileNegativeTaskPlan(context.Background(),after,intent.Params(),source,old,capacity);err==nil {t.Fatal("old qualification accepted new Params")}
 plan,qualified,err:=intent.CompileNegative(context.Background(),old.pin,old.locks,old.reportRaw,old.calibrationRaw,source,capacity)
 if err!=nil||!bytes.Equal(plan.Params,intent.Params())||plan.ParamsSHA!=intent.SHA256()||qualified.params.Base!=after {t.Fatal("fresh replacement compile",err)}
 bad:=append(append([]byte(nil),source...),byte(' '))
 if plan,_,err:=intent.CompileNegative(context.Background(),old.pin,old.locks,old.reportRaw,old.calibrationRaw,bad,capacity);err==nil||len(plan.Params)!=0 {t.Fatal("substituted source accepted")}
 corrupt:=intent;corrupt.digest="invalid"
 if _,_,err:=corrupt.CompileNegative(context.Background(),old.pin,old.locks,old.reportRaw,old.calibrationRaw,source,capacity);err!=v2input.ErrInvalid {t.Fatal("corrupt intent accepted",err)}
}
func TestReplacementMultiRequalifiesOrderedReboundFrames(t *testing.T){
 for _,op:=range []string{"optical.panorama","optical.focus_stack"} {
  t.Run(op,func(t *testing.T){pin,locks,report,calibration,raw,originals:=multiQualifiedFixture(t,op)
   var before v2input.SessionVersionBinding
   if op=="optical.panorama" {p,err:=ParsePanoramaParams(raw);if err!=nil {t.Fatal(err)};before=p.Frames[0]} else {p,err:=ParseFocusStackParams(raw);if err!=nil {t.Fatal(err)};before=p.Frames[0]}
   after:=before;after.Source.ArtifactID=uuid.New()
   intent,err:=RebindOpticalIntent(context.Background(),op,raw,[]OpticalReplacementBinding{{Before:before,After:after}},nil);if err!=nil {t.Fatal(err)}
   capacity,err:=MultiResources(op,intent.Params(),originals);if err!=nil {t.Fatal(err)}
   plan,qualified,err:=intent.CompileMulti(context.Background(),pin,locks,report,calibration,originals,capacity)
   if err!=nil||plan.Operation!=op||plan.ParamsSHA!=intent.SHA256()||!bytes.Equal(plan.Params,intent.Params())||qualified.paramsRaw!=string(intent.Params()) {t.Fatal("fresh multi compile",err)}
   reversed:=append([][]byte(nil),originals...);reversed[0]=[]byte("{}")
   if plan,_,err:=intent.CompileMulti(context.Background(),pin,locks,report,calibration,reversed,capacity);err==nil||len(plan.Params)!=0 {t.Fatal("wrong source frame accepted")}
  })
 }
}
