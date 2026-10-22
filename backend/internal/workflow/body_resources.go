package workflow

import (
 "encoding/json"
 "errors"
 "photoeditor/backend/internal/canonical"
)

// Include the inverse field, RGB, alpha, source inputs and bounded reports.
func bodyResourceEstimate(pixels,inputBytes int64,alpha bool)(ResourceEstimate,error){
 if pixels<1||pixels>40000000||inputBytes<0||inputBytes>1048576{return ResourceEstimate{},errors.New("invalid body resource scope")}
 bpp:=int64(20);if alpha{bpp=22}
 ram:=int64(3<<30);if need:=72*pixels+(128<<20);need>ram{ram=need}
 return ResourceEstimate{RAMBytes:ram,VRAMBytes:0,ScratchBytes:48*pixels+inputBytes+(8<<20),OutputBytes:bpp*pixels+(4<<20),PixelCount:pixels},nil
}

func applyBodyProfile(pixels,inputBytes int64,alpha bool)(json.RawMessage,error){
 estimate,err:=bodyResourceEstimate(pixels,inputBytes,alpha);if err!=nil{return nil,err}
 return canonical.Marshal(struct{
 Backend string `json:"backend"`
 Precision string `json:"precision"`
 Tile int `json:"tile_size"`
 Estimate ResourceEstimate `json:"resource_estimate"`
 }{"cpu","float32",512,estimate})
}
