package semanticproposal
import("testing";"strings";"photoeditor/backend/internal/v2qualification";"photoeditor/backend/internal/v2input")
func TestQualifiedResourceCostsPreserveMPSConservativeBound(t *testing.T){
 r:=v2qualification.ResourceEvidence{RAMBytes:1024,GPUBytes:20000000000,ScratchBytes:2048,GPUKind:v2qualification.MPSAllocatorMetric};costs,err:=transferCostsFromResources(r);if err!=nil||costs.VRAMBytes!=20000000000{t.Fatal(costs,err)}
 if err:=validateTransferResourceCosts(costs,r);err!=nil{t.Fatal(err)}
 for _,field:=range []string{"ram","gpu","scratch"}{t.Run(field,func(t *testing.T){changed:=costs;switch field{case "ram":changed.RAMBytes--;case "gpu":changed.VRAMBytes--;case "scratch":changed.ScratchBytes--};if validateTransferResourceCosts(changed,r)==nil{t.Fatal("understated bound accepted")}})}
 for _,kind:=range []string{"","sampled_peak","exact_mps_peak"}{r.GPUKind=kind;if _,err:=transferCostsFromResources(r);err==nil{t.Fatal("unknown resource provenance admitted")}}
 if validateTransferResourceCosts(v2input.SemanticQualifiedCosts{},v2qualification.ResourceEvidence{})==nil{t.Fatal("zero evidence admitted")}
}

func TestQualifiedResourceSealDoesNotExposeMPSAsMeasuredMetrics(t *testing.T){
 r:=v2qualification.ResourceEvidence{RAMBytes:1024,GPUBytes:20000000000,ScratchBytes:2048,GPUKind:v2qualification.MPSAllocatorMetric}
 if _,ok:=qualificationResourceSeal(r).(v2qualification.Metrics);ok{t.Fatal("MPS seal fabricated a measured report")}
 if sealed,ok:=qualificationResourceSeal(r).(v2qualification.ResourceEvidence);!ok||sealed!=r{t.Fatal("MPS resource discriminator lost")}
}

func TestQualifiedMetalBudgetsRespectCapacityNotFixedTenGB(t *testing.T){
 capacity:=TransferPreparation{Color:TransferColorProfile{sha:strings.Repeat("a",64)},RAM:1<<30,VRAM:30000000000,Scratch:1<<30,Output:1<<20,MetadataBytes:16<<20}
 r:=v2qualification.ResourceEvidence{RAMBytes:1024,GPUBytes:20000000000,ScratchBytes:2048,GPUKind:v2qualification.MPSAllocatorMetric}
 face,err:=initialNativeFaceResourceBudget(capacity,16<<20,r);if err!=nil||face.Costs.VRAMBytes!=20000000000{t.Fatal(face,err)}
 person,err:=createPersonResourceBudget(capacity,16<<20,r);if err!=nil||person.Costs!=face.Costs{t.Fatal(person,err)}
 capacity.VRAM=r.GPUBytes-1;if _,err:=initialNativeFaceResourceBudget(capacity,16<<20,r);err==nil{t.Fatal("capacity overflow admitted")}
 if _,err:=createPersonResourceBudget(capacity,8<<20,r);err==nil{t.Fatal("missing transport reserve admitted")}
}
