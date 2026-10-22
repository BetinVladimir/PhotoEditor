package semanticproposal
import("photoeditor/backend/internal/v2qualification";"photoeditor/backend/internal/v2input")
func transferCostsFromResources(r v2qualification.ResourceEvidence)(v2input.SemanticQualifiedCosts,error){
 if !r.Valid(){return v2input.SemanticQualifiedCosts{},v2input.ErrInvalid}
 return v2input.SemanticQualifiedCosts{RAMBytes:r.RAMBytes,VRAMBytes:r.GPUBytes,ScratchBytes:r.ScratchBytes},nil
}
func validateTransferResourceCosts(c v2input.SemanticQualifiedCosts,r v2qualification.ResourceEvidence)error{
 if !r.Valid(){return v2input.ErrInvalid}
 if err:=validateTransferMeasuredCosts(c,v2qualification.Metrics{});err!=nil{return err}
 if c.RAMBytes<r.RAMBytes||c.VRAMBytes<r.GPUBytes||c.ScratchBytes<r.ScratchBytes{return v2input.ErrInvalid};return nil
}
func initialNativeFaceResourceBudget(capacity TransferPreparation,reserved int64,r v2qualification.ResourceEvidence)(nativePreparationBudget,error){
 var zero nativePreparationBudget
 if err:=validateTransferCapacityConfiguration(capacity);err!=nil{return zero,err}
 costs,err:=transferCostsFromResources(r);if err!=nil{return zero,err}
 if costs.RAMBytes>capacity.RAM||costs.VRAMBytes>capacity.VRAM||costs.ScratchBytes>capacity.Scratch{return zero,v2input.ErrInvalid}
 budget:=nativePreparationBudget{Costs:costs,RAM:capacity.RAM,VRAM:capacity.VRAM,Scratch:capacity.Scratch,Output:capacity.Output,Reserved:reserved}
 if err:=validateNativeFaceStartBudget(budget);err!=nil{return zero,err};return budget,nil
}
func createPersonResourceBudget(capacity TransferPreparation,reserved int64,r v2qualification.ResourceEvidence)(nativePreparationBudget,error){
 if err:=validateTransferCapacityConfiguration(capacity);err!=nil{return nativePreparationBudget{},err}
 if reserved<=8<<20{return nativePreparationBudget{},v2input.ErrInvalid}
 capacity=ownTransferPreparation(capacity);if capacity.Output>reserved-(8<<20){capacity.Output=reserved-(8<<20)}
 return initialNativeFaceResourceBudget(capacity,reserved,r)
}

// Legacy plan fingerprints retain their previous exact measured representation.
// MPS fingerprints carry explicitly typed allocator-bound resource evidence.
func qualificationResourceSeal(r v2qualification.ResourceEvidence)any{
 if measured,ok:=r.MeasuredMetrics();ok{return measured};return r
}
