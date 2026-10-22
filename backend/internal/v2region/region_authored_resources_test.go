package v2region

import "testing"

func TestAuthoredCropResourceBudgetAccountsFullCanvasAndPersistentIndex(t *testing.T){
 a,err:=CompileAuthoredCropResources(6000,4000,128<<20,1024,true)
 if err!=nil{t.Fatal(err)}
 if a.PixelCount!=24000000||a.VRAMBytes!=0||a.RAMBytes<24000000*96+(128<<20){t.Fatalf("full canvas omitted: %+v",a)}
 if a.ScratchBytes<(128<<20)+(16<<20)+2*25165824||a.OutputBytes!=25165824{t.Fatalf("persistent input index or both outputs omitted: %+v",a)}
 b,err:=CompileAuthoredCropResources(6000,4000,128<<20,1024,false)
 if err!=nil||b.RAMBytes>=a.RAMBytes{t.Fatalf("opaque estimate does not follow actual presentation implementation: %+v %v",b,err)}
}
func TestAuthoredCropResourcesRejectMalformedOrUnboundedInputs(t *testing.T){
 for _,v:=range []struct{w,h int64;input int64;edge int}{{0,1,2,64},{1,0,2,64},{20001,20000,2,64},{1,1,0,64},{1,1,-1,64},{1,1,2,63},{1,1,2,1025},{1,1,9223372036854775807,64}}{
  if _,err:=CompileAuthoredCropResources(v.w,v.h,v.input,v.edge,true);err==nil{t.Fatalf("accepted %+v",v)}
 }
}
