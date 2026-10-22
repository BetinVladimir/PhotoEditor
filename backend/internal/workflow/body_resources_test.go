package workflow
import "testing"

func TestBodyResourcesIncludePhysicalFieldAndAlpha(t *testing.T){
 for _,alpha:=range []bool{false,true}{
  e,err:=bodyResourceEstimate(40000000,1048576,alpha);if err!=nil{t.Fatal(err)}
  bpp:=int64(20);if alpha{bpp=22}
  if e.OutputBytes<=bpp*40000000||e.ScratchBytes<e.OutputBytes+18*40000000||e.RAMBytes<72*40000000+(128<<20)||e.VRAMBytes!=0{t.Fatalf("underestimated body: %+v",e)}
 }
}
func TestBodyResourcesRejectInvalidScope(t *testing.T){
 for _,v:=range [][2]int64{{0,1},{40000001,1},{1,-1},{1,1048577}}{if _,err:=bodyResourceEstimate(v[0],v[1],false);err==nil{t.Fatal("invalid scope accepted")}}
}
