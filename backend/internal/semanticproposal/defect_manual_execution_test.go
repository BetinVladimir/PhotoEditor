package semanticproposal

import "testing"

func TestDefectManualExecutionOwnsExternalVector(t *testing.T) {
	original := manualPinTestProof()
	vector := original.objects.Params
	original.vectors = &vector
	retained := ownDefectManualExecutionProof(original)
	if retained.proof.vectors != nil || !retained.hasVector || retained.vector != vector {
		t.Fatal("vector not owned")
	}
	vector.object.VersionID = "mutated"
	recovered := retained.original()
	if recovered.vectors == nil || recovered.vectors.object.VersionID == "mutated" {
		t.Fatal("original vector alias escaped")
	}
	recovered.vectors.object.VersionID = "changed-again"
	if retained.original().vectors.object.VersionID == "changed-again" {
		t.Fatal("decoded vector alias escaped")
	}
	inline := manualPinTestProof()
	if owned := ownDefectManualExecutionProof(inline); owned.hasVector || owned.original().vectors != nil {
		t.Fatal("inline vector acquired external reference")
	}
}
