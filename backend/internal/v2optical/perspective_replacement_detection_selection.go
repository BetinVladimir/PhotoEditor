package v2optical

import (
 "context"
 "photoeditor/backend/internal/v2input"
)

// AutomaticPerspectiveDetection identifies supported frozen intent without
// treating an unrelated operation as a failed automatic detector candidate.
// This is selection only; PerspectiveDetectionSource still verifies mappings.
func(p PreparedMappedReplacementStep) AutomaticPerspectiveDetection(ctx context.Context)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if p.parents.step.Operation()!="optical.perspective"{return false,nil}
 frozen,err:=ParsePerspectiveParams([]byte(p.parents.step.intent.raw))
 if err!=nil{return false,err}
 if frozen.Mode!="auto"&&frozen.Mode!="manual"{return false,v2input.ErrInvalid}
 return frozen.Mode=="auto",nil
}
