package httpapi

import (
	"github.com/google/uuid"
	"testing"
)

func TestImportMaskStatusesRejectInvalidOrDuplicateEvidence(t *testing.T) {
	valid := importMaskConfirmationStatus{EntryID: uuid.New(), OverrideID: uuid.New(), MaskID: uuid.New(), TargetVersionID: uuid.New(), Status: "granted", Epoch: 1}
	if err := validateImportMaskStatuses([]importMaskConfirmationStatus{valid}); err != nil {
		t.Fatal(err)
	}
	for _, epoch := range []int64{0, -1, 9007199254740992} {
		item := valid
		item.Epoch = epoch
		if err := validateImportMaskStatuses([]importMaskConfirmationStatus{item}); err == nil {
			t.Fatalf("accepted epoch %d", epoch)
		}
	}
	if err := validateImportMaskStatuses([]importMaskConfirmationStatus{valid, valid}); err == nil {
		t.Fatal("accepted duplicate")
	}
	item := valid
	item.Status = "pending"
	if err := validateImportMaskStatuses([]importMaskConfirmationStatus{item}); err == nil {
		t.Fatal("accepted unknown status")
	}
	item = valid
	item.TargetVersionID = uuid.Nil
	if err := validateImportMaskStatuses([]importMaskConfirmationStatus{item}); err == nil {
		t.Fatal("accepted nil target")
	}
}
