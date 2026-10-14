package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/objectstore"
)

func TestExportPersonOriginCurrentCheckerBindsSignedProducer(t *testing.T) {
	raw, scope, keys := portablePersonOriginFixture(t)
	claim, err := VerifyPortablePersonOrigin(raw, scope, keys)
	if err != nil {
		t.Fatal(err)
	}
	locator := &personOriginLocatorStub{origin: ExportSeriesMaskOrigin{
		Task: claim.ProducerTaskID, Operation: claim.Operation, Version: claim.OperationVersion,
		Attempt: claim.ProducerAttempt, Fence: claim.ProducerFence,
		Root:   ExportSeriesMaskDocument{Ref: claim.MaskRef},
		Report: ExportSeriesMaskDocument{Ref: claim.QualificationReport},
		Mask:   objectstore.Object{SHA256: claim.MaskPixels.SHA256},
	}}
	checker := SQLExportPersonOriginCurrentChecker{Locator: locator}
	if err := checker.RecheckExportPersonOrigin(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	locator.change = true
	if err := checker.RecheckExportPersonOrigin(context.Background(), claim); err == nil {
		t.Fatal("changed producer fence admitted")
	}
}
