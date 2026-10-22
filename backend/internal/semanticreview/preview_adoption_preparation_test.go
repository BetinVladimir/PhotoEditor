package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestShownPreparationRejectsMissingInstallerBeforePublication(t *testing.T) {
	child, draft, selected, physical := shownCompileFixture(t)
	recorder := &previewPublicationRecorder{}
	prepared, err := PrepareShownExposure(context.Background(), child, draft, selected, physical, nil, nil, ExposureSourceDocuments{},
		ExposureOwnerCosts{}, ExposureResourceEstimate{}, ExposureQualificationPin{}, recorder, nil)
	if err == nil || len(recorder.calls) != 0 || prepared.documents != nil {
		t.Fatal("missing authority accepted")
	}
}
func TestShownBundleRejectsIncompletePhysicalDocuments(t *testing.T) {
	_, draft, _, physical := shownCompileFixture(t)
	if raw, err := shownExposureDocumentBundle(draft, physical, nil, nil, ExposureSourceDocuments{}); err == nil || raw != nil {
		t.Fatal("incomplete bundle")
	}
}
func TestShownPreflightCancellationBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &shownLockTX{}
	err := CheckPreparedShownExposure(ctx, tx, auth.Identity{}, uuid.New(), PreparedShownExposure{})
	if err != context.Canceled || tx.calls != 0 {
		t.Fatal("cancelled preflight reached SQL")
	}
}

func TestShownPreparationFactoryRejectsMissingAuthorityBeforeFactory(t *testing.T) {
	child, draft, selected, physical := shownCompileFixture(t)
	calls := 0
	factory := func(FrozenShownExposureTask) (v2input.Publisher, error) { calls++; return nil, nil }
	_, err := PrepareShownExposureWithPublisher(context.Background(), child, draft, selected, physical, nil, nil,
		ExposureSourceDocuments{}, ExposureOwnerCosts{}, ExposureResourceEstimate{}, ExposureQualificationPin{}, factory, nil)
	if err == nil || calls != 0 {
		t.Fatal("factory called without authority")
	}
}
