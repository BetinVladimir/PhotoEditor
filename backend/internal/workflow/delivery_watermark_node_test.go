package workflow

import (
 "math"
 "testing"
)
func TestDeliveryWatermarkResourcesIncludeBothSourcesAndAlpha(t *testing.T){
 unlimited:=DeliveryDerivativeLimits{RAMBytes:math.MaxInt64,ScratchBytes:math.MaxInt64,OutputBytes:math.MaxInt64}
 rgb,err:=compileDeliveryWatermarkEstimate(80000,5000,false,unlimited);if err!=nil{t.Fatal(err)}
 alpha,err:=compileDeliveryWatermarkEstimate(80000,5000,true,unlimited);if err!=nil{t.Fatal(err)}
 if rgb.RAMBytes!=(256<<20)+192*85000||rgb.ScratchBytes!=(32<<20)+12*80000+16*5000||rgb.OutputBytes!=(8<<20)+12*80000||rgb.VRAMBytes!=0||rgb.PixelCount!=80000{t.Fatal("missing watermark allocation")}
 if alpha.ScratchBytes-rgb.ScratchBytes!=4*80000||alpha.OutputBytes-rgb.OutputBytes!=4*80000{t.Fatal("alpha allocation")}
 exact:=DeliveryDerivativeLimits{RAMBytes:rgb.RAMBytes,ScratchBytes:rgb.ScratchBytes,OutputBytes:rgb.OutputBytes}
 if _,err=compileDeliveryWatermarkEstimate(80000,5000,false,exact);err!=nil{t.Fatal("exact reservation",err)}
 for _,field:=range []string{"ram","scratch","output"}{
  limited:=exact;switch field{case "ram":limited.RAMBytes--;case "scratch":limited.ScratchBytes--;case "output":limited.OutputBytes--}
  if _,err=compileDeliveryWatermarkEstimate(80000,5000,false,limited);err==nil{t.Fatal("insufficient reservation",field)}
 }
 for _,size:=range [][2]int64{{0,1},{1,0},{200000001,1},{1,200000001},{math.MaxInt64,math.MaxInt64}}{
  if _,err=compileDeliveryWatermarkEstimate(size[0],size[1],false,unlimited);err==nil{t.Fatal("invalid geometry accepted",size)}
 }
}
