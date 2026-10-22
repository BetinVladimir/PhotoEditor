package semanticproposal

import (
 "testing"
 "photoeditor/backend/internal/v2input"
)
func TestSemanticSceneCostsRetainMeasuredPeaksAndBothPhysicalSources(t *testing.T) {
 measured:=v2input.SemanticQualifiedCosts{RAMBytes:123,VRAMBytes:456,ScratchBytes:789}
 got,err:=semanticSceneCosts(100,80,120,90,512,measured)
 if err!=nil || got.RAMBytes!=123+(512<<20)+256*512*512 || got.ScratchBytes!=789+10*(100*80+120*90)+12*8192 || got.VRAMBytes!=456 {t.Fatalf("scene additive reservation: %+v %v",got,err)}
 if measured.RAMBytes!=123 || measured.ScratchBytes!=789 {t.Fatal("caller cost mutated")}
}
func TestSemanticSceneCostsRejectInvalidShapesProxyAndOverflow(t *testing.T) {
 for _, shape:=range [][5]int64{{7,8,8,8,512},{200000000,200000000,8,8,512},{8,8,8,8,31},{8,8,8,8,1025}} {
  if _,err:=semanticSceneCosts(shape[0],shape[1],shape[2],shape[3],int(shape[4]),v2input.SemanticQualifiedCosts{});err==nil {t.Fatal("invalid scene dimensions accepted")}
 }
 for _, costs:=range []v2input.SemanticQualifiedCosts{{RAMBytes:9007199254740991},{ScratchBytes:9007199254740991},{VRAMBytes:-1}} {
  if _,err:=semanticSceneCosts(8,8,8,8,512,costs);err==nil {t.Fatal("invalid scene costs accepted")}
 }
}
