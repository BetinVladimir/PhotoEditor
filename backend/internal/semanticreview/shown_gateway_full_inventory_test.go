package semanticreview

import (
	"context"
	"testing"
)

// A real shown compiler inventory has several identically typed dependencies.
// Gateway closure projections must choose output refs by the canonical selected
// receipt, not assume a unique WorkingImage/NpyArray contract in the inventory.
func TestShownGatewayProjectionHasFullProductionInventory(t *testing.T) {
	f := fullShownRemovalContentFixture(t)
	compiled, err := CompileShownRemovalAdoption(f.child, f.approved, f.selected, f.physical)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := CompileShownRemovalAdoptionInventory(compiled, f.approved, f.physical, f.candidate, f.intent, f.docs)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := closure.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	outputImage, outputPixels := false, false
	for {
		entry, more, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
		counts[entry.Contract]++
		if entry.Contract == "WorkingImage" && entry.Ref == f.selected.Receipt.WorkingImage {
			outputImage = true
		}
		if entry.Contract == "NpyArray" && entry.Ref == f.physical.linkage.Presentation.Pixels {
			outputPixels = true
		}
	}
	if counts["WorkingImage"] < 2 || counts["NpyArray"] < 2 || counts["ApprovedSemanticEdit"] != 1 || !outputImage || !outputPixels {
		t.Fatal("full production inventory does not preserve ambiguous types and exact selected outputs", counts, outputImage, outputPixels)
	}
}
