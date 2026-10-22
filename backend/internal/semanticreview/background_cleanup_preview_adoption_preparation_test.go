package semanticreview

import (
	"context"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestShownBackgroundCleanupPreparationRejectsMissingAuthorityBeforeFactory(t *testing.T) {
	child, draft, selected, physical := shownBackgroundCleanupCompileFixture(t)
	calls := 0
	factory := func(FrozenShownBackgroundCleanupTask) (v2input.Publisher, error) { calls++; return nil, nil }
	got, err := PrepareShownBackgroundCleanupWithPublisher(context.Background(), child, draft, selected, physical, nil, nil, ExposureSourceDocuments{}, ExposureOwnerCosts{}, ExposureResourceEstimate{}, ExposureQualificationPin{}, factory, nil)
	if err == nil || calls != 0 || got.documents != nil {
		t.Fatal("unadmitted publication", err)
	}
}

func TestShownBackgroundCleanupBundleRejectsMissingPhysicalDocuments(t *testing.T) {
	_, draft, _, physical := shownBackgroundCleanupCompileFixture(t)
	raw, err := shownBackgroundCleanupDocumentBundle(draft, physical, nil, nil, ExposureSourceDocuments{})
	if err == nil || raw != nil {
		t.Fatal("incomplete bundle admitted")
	}
}

func TestShownBackgroundCleanupPreparationCancellationBeforeCallbacks(t *testing.T) {
	child, draft, selected, physical := shownBackgroundCleanupCompileFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	got, err := PrepareShownBackgroundCleanupWithPublisher(ctx, child, draft, selected, physical, nil, nil, ExposureSourceDocuments{}, ExposureOwnerCosts{}, ExposureResourceEstimate{}, ExposureQualificationPin{}, func(FrozenShownBackgroundCleanupTask) (v2input.Publisher, error) { calls++; return nil, nil }, func(context.Context) error { calls++; return nil })
	if err != context.Canceled || calls != 0 || got.documents != nil {
		t.Fatal("cancelled preparation reached callbacks", err, calls)
	}
}
