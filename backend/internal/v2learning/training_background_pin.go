package v2learning

import (
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Structural frozen evidence only. SQL style/source authority and task closure
// admission must independently accept the exact source before worker execution.
func validTrainingBackgroundPin(pin *v2draft.FrozenBackgroundAsset,icc string,style v2input.Ref)bool{
 if pin==nil{return true}
 raw,err:=canonical.Marshal(pin);if err!=nil{return false}
 decoded,err:=v2draft.DecodeFrozenBackgroundAsset(raw);if err!=nil{return false}
 return (icc==""||decoded.WorkICCSHA==icc)&&(style==(v2input.Ref{})||decoded.Style==style)
}

// Until encoded dimensions are independently frozen, admit the decoder's full
// 200MP ceiling. Includes source bytes, normalized RGB and bounded tile/native
// headroom; retained frame/metric/model resources are added by Evaluate.
func trainingBackgroundDecodeResources(pin *v2draft.FrozenBackgroundAsset)(int64,int64,error){
 if pin==nil{return 0,0,nil}
 if !validTrainingBackgroundPin(pin,"",v2input.Ref{}){return 0,0,ErrInvalidTrainingDraft}
 const pixels int64=200_000_000
 ram:=12*pixels+pin.ExpectedBytes+1024*1024*1024+128*512*512
 scratch:=pin.ExpectedBytes+12*pixels+1048576
 return ram,scratch,nil
}
