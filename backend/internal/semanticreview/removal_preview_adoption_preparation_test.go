package semanticreview

import (
	"context"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestShownRemovalPreparationRejectsMissingAuthorityBeforeFactory(t *testing.T) {
	child, draft, selected, physical := shownRemovalCompileFixture(t)
	calls := 0
	factory := func(FrozenShownRemovalTask) (v2input.Publisher, error) { calls++; return nil, nil }
	got, err := PrepareShownRemovalWithPublisher(context.Background(), child, draft, selected, physical, nil, nil, ExposureSourceDocuments{}, ExposureOwnerCosts{}, ExposureResourceEstimate{}, ExposureQualificationPin{}, factory, nil)
	if err == nil || calls != 0 || got.documents != nil {
		t.Fatal("unadmitted publication", err)
	}
}

func TestShownRemovalBundleRejectsMissingPhysicalDocuments(t *testing.T) {
	_, draft, _, physical := shownRemovalCompileFixture(t)
	raw, err := shownRemovalDocumentBundle(draft, physical, nil, nil, ExposureSourceDocuments{})
	if err == nil || raw != nil {
		t.Fatal("incomplete bundle admitted")
	}
}
