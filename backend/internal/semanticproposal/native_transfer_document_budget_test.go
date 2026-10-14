package semanticproposal

import (
	"math"
	"testing"
)

func TestNativeTransferDocumentBudgetWholeBatchBoundaries(t *testing.T) {
	objects := make([]personEvidenceObject, 3)
	for i := range objects {
		objects[i].object.Bytes = 8 << 20
	}
	used, err := nativeTransferDocumentBudget(objects, 24<<20)
	if err != nil || used != 24<<20 {
		t.Fatal("exact complete budget", used, err)
	}
	for _, limit := range []int64{0, -1, (24 << 20) - 1, math.MaxInt64} {
		if used, err := nativeTransferDocumentBudget(objects, limit); err == nil || used != 0 {
			t.Fatal("invalid/partial budget accepted", limit, used, err)
		}
	}
	for _, size := range []int64{0, 1, -1, (8 << 20) + 1, math.MaxInt64} {
		changed := append([]personEvidenceObject(nil), objects...)
		changed[2].object.Bytes = size
		if used, err := nativeTransferDocumentBudget(changed, 24<<20); err == nil || used != 0 {
			t.Fatal("unbounded document accepted", size, used, err)
		}
	}
	for _, count := range []int{0, 1, 2, 4} {
		if used, err := nativeTransferDocumentBudget(make([]personEvidenceObject, count), 24<<20); err == nil || used != 0 {
			t.Fatal("partial/extra document set accepted", count)
		}
	}
}
