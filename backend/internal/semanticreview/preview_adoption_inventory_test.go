package semanticreview

import "testing"

func TestShownAdoptionInventoryRejectsAlteredFrozenCompilation(t *testing.T) {
	child, draft, selected, physical := shownCompileFixture(t)
	compiled, err := CompileShownExposureAdoption(child, draft, selected, physical)
	if err != nil {
		t.Fatal(err)
	}
	compiled.body = append(compiled.body, ' ')
	closure, err := CompileShownExposureAdoptionInventory(compiled, draft, physical, nil, nil, ExposureSourceDocuments{})
	if err == nil || closure.Open != nil {
		t.Fatal("altered compiled bytes admitted")
	}
}
func TestShownAdoptionInventoryRejectsMissingProposalDocuments(t *testing.T) {
	child, draft, selected, physical := shownCompileFixture(t)
	compiled, err := CompileShownExposureAdoption(child, draft, selected, physical)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := CompileShownExposureAdoptionInventory(compiled, draft, physical, nil, nil, ExposureSourceDocuments{})
	if err == nil || closure.Open != nil {
		t.Fatal("missing source closure admitted")
	}
}
