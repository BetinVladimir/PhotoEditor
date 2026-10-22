package semanticreview

import "testing"

func TestShownRemovalAdoptionInventoryRejectsAlteredFrozenCompilation(t *testing.T) {
	child, draft, selected, physical := shownRemovalCompileFixture(t)
	compiled, err := CompileShownRemovalAdoption(child, draft, selected, physical)
	if err != nil {
		t.Fatal(err)
	}
	compiled.body = append(compiled.body, ' ')
	closure, err := CompileShownRemovalAdoptionInventory(compiled, draft, physical, nil, nil, ExposureSourceDocuments{})
	if err == nil || closure.Open != nil {
		t.Fatal("altered compiled bytes admitted")
	}
}
func TestShownRemovalAdoptionInventoryRejectsMissingProposalDocuments(t *testing.T) {
	child, draft, selected, physical := shownRemovalCompileFixture(t)
	compiled, err := CompileShownRemovalAdoption(child, draft, selected, physical)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := CompileShownRemovalAdoptionInventory(compiled, draft, physical, nil, nil, ExposureSourceDocuments{})
	if err == nil || closure.Open != nil {
		t.Fatal("missing source closure admitted")
	}
}
