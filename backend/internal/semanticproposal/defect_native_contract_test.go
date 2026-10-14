package semanticproposal

import "testing"

func TestDefectNativeInputContractPreservesDocumentAndGeometryRoles(t *testing.T) {
	for _, contract := range []string{"WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarp", "DenseWarpField"} {
		if got := defectNativeInputContract(contract); got != "NpyArray" {
			t.Fatalf("%s mapped to %s", contract, got)
		}
	}
	for _, contract := range []string{"WorkingImage", "FaceLandmarks", "MaskRef", "Params", "Unsupported"} {
		if got := defectNativeInputContract(contract); got != contract {
			t.Fatalf("document role %s changed to %s", contract, got)
		}
	}
}
