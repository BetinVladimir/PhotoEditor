package v2optical

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func(i ReboundOpticalIntent)validFor(operation string)bool{
 if i.operation!=operation||len(i.raw)<2||len(i.raw)>65536 {return false}
 sum:=sha256.Sum256([]byte(i.raw));return hex.EncodeToString(sum[:])==i.digest
}

// These compilers repeat content qualification for rebound Params/source bytes.
// Current SQL qualification, physical closure and queue admission are separate.
// A previous execution profile is neither accepted nor copied.
func(i ReboundOpticalIntent)CompileNegative(ctx context.Context,pin NegativeQualificationPin,locks v2qualification.DeploymentLocks,report,calibration,source []byte,capacity NegativeResourceEstimate)(NegativeTaskPlan,VerifiedNegativeQualification,error){
 var plan NegativeTaskPlan;var qualified VerifiedNegativeQualification
 if err:=ctx.Err();err!=nil {return plan,qualified,err}
 if !i.validFor("optical.negative") {return plan,qualified,v2input.ErrInvalid}
 params,err:=ParseNegativeParams([]byte(i.raw));if err!=nil {return plan,qualified,err}
 image,err:=review.ParseWorkingImage(source);if err!=nil {return plan,qualified,v2input.ErrInvalid}
 qualified,err=VerifyNegativeQualificationEvidence(pin,locks,report,calibration,params,image.Color.ICCSHA256);if err!=nil {return plan,VerifiedNegativeQualification{},err}
 plan,err=CompileNegativeTaskPlan(ctx,params.Base,[]byte(i.raw),append([]byte(nil),source...),qualified,capacity)
 if err!=nil {return NegativeTaskPlan{},VerifiedNegativeQualification{},err};return plan,qualified,nil
}
func(i ReboundOpticalIntent)CompilePerspective(ctx context.Context,pin PerspectiveQualificationPin,locks v2qualification.DeploymentLocks,report,calibration,source []byte,maxPixels int64,capacity PerspectiveResourceEstimate,lines ...[]byte)(PerspectiveTaskPlan,VerifiedPerspectiveQualification,error){
 var plan PerspectiveTaskPlan;var qualified VerifiedPerspectiveQualification
 if err:=ctx.Err();err!=nil {return plan,qualified,err}
 if !i.validFor("optical.perspective") {return plan,qualified,v2input.ErrInvalid}
 params,err:=ParsePerspectiveParams([]byte(i.raw));if err!=nil {return plan,qualified,err}
 image,err:=review.ParseWorkingImage(source);if err!=nil {return plan,qualified,v2input.ErrInvalid}
 qualified,err=VerifyPerspectiveQualificationEvidence(pin,locks,report,calibration,params,image.Color.ICCSHA256);if err!=nil {return plan,VerifiedPerspectiveQualification{},err}
 owned:=make([][]byte,len(lines));for j,raw:=range lines {owned[j]=append([]byte(nil),raw...)}
 plan,err=CompilePerspectiveTaskPlan(ctx,params.Base,[]byte(i.raw),append([]byte(nil),source...),qualified,maxPixels,capacity,owned...)
 if err!=nil {return PerspectiveTaskPlan{},VerifiedPerspectiveQualification{},err};return plan,qualified,nil
}
func(i ReboundOpticalIntent)CompileMulti(ctx context.Context,pin MultiQualificationPin,locks v2qualification.DeploymentLocks,report,calibration []byte,originals [][]byte,capacity MultiResourceEstimate)(MultiTaskPlan,VerifiedMultiQualification,error){
 var plan MultiTaskPlan;var qualified VerifiedMultiQualification
 if err:=ctx.Err();err!=nil {return plan,qualified,err}
 if (i.operation!="optical.panorama"&&i.operation!="optical.focus_stack")||!i.validFor(i.operation) {return plan,qualified,v2input.ErrInvalid}
 owned:=make([][]byte,len(originals));for j,raw:=range originals {owned[j]=append([]byte(nil),raw...)}
 qualified,err:=VerifyMultiQualificationEvidence(i.operation,pin,locks,report,calibration,[]byte(i.raw),owned);if err!=nil {return plan,VerifiedMultiQualification{},err}
 plan,err=CompileMultiTaskPlan(ctx,i.operation,[]byte(i.raw),owned,qualified,capacity)
 if err!=nil {return MultiTaskPlan{},VerifiedMultiQualification{},err};return plan,qualified,nil
}
