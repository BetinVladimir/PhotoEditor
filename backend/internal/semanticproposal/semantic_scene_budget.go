package semanticproposal

import "photoeditor/backend/internal/v2input"

// semanticSceneCosts reserves scene verification while RGB/protection buffers
// remain alive. It adds to measured transfer costs; it does not replace them or
// confer model/producer authority. Invalid depth masks are charged for both
// sources even when absent, so later optional-mask binding cannot shrink a lease.
func semanticSceneCosts(referenceWidth, referenceHeight, targetWidth, targetHeight int64, proxyEdge int, measured v2input.SemanticQualifiedCosts) (v2input.SemanticQualifiedCosts, error) {
 const safe int64 = 9007199254740991
 for _, shape := range [][2]int64{{referenceWidth, referenceHeight}, {targetWidth, targetHeight}} {
  if shape[0]<8 || shape[1]<8 || shape[0]>200000000 || shape[1]>200000000 || shape[0]*shape[1]>200000000 { return v2input.SemanticQualifiedCosts{},v2input.ErrInvalid }
 }
 if proxyEdge<32 || proxyEdge>1024 { return v2input.SemanticQualifiedCosts{},v2input.ErrInvalid }
 for _, value := range []int64{measured.RAMBytes,measured.VRAMBytes,measured.ScratchBytes} { if value<0 || value>safe {return v2input.SemanticQualifiedCosts{},v2input.ErrInvalid} }
 pixels:=referenceWidth*referenceHeight+targetWidth*targetHeight
 ram:=int64(512<<20)+256*int64(proxyEdge)*int64(proxyEdge)
 // uint16 labels + float32 depth + conservative float32 invalid masks,
 // plus bounded document/array headers for both source analyses.
 scratch:=10*pixels+12*8192
 if measured.RAMBytes>safe-ram || measured.ScratchBytes>safe-scratch { return v2input.SemanticQualifiedCosts{},v2input.ErrInvalid }
 measured.RAMBytes+=ram
 measured.ScratchBytes+=scratch
 return measured,nil
}
