package delivery

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestMergeSignedAssemblyInventoryPreservesTransitiveInputs(t *testing.T) {
	entry := func(id uuid.UUID, contract string) v2input.Entry {
		return v2input.Entry{Ref: v2input.Ref{ArtifactID: id,
			SHA256: strings.Repeat("a", 64)}, Contract: contract}
	}
	base := []v2input.Entry{entry(uuid.New(), "DeliveryAssemblyInput"),
		entry(uuid.New(), "WorkingImage"), entry(uuid.New(), "EncodedExport")}
	signed := []v2input.Entry{base[0], base[2], entry(uuid.New(), "SignedExport")}
	merged, err := MergeSignedAssemblyInventory(base, signed)
	if err != nil || len(merged) != 4 {
		t.Fatalf("complete signed inventory: entries=%d err=%v", len(merged), err)
	}
	for i := 1; i < len(merged); i++ {
		if merged[i-1].Ref.ArtifactID.String() >= merged[i].Ref.ArtifactID.String() {
			t.Fatal("inventory is not sorted by artifact ID")
		}
	}
	changed := base[2]
	changed.Ref.SHA256 = strings.Repeat("b", 64)
	if _, err := MergeSignedAssemblyInventory(base, []v2input.Entry{base[0], changed,
		entry(uuid.New(), "C2PAManifest")}); err == nil {
		t.Fatal("conflicting digest accepted")
	}
	changed = base[2]
	changed.Contract = "SignedExport"
	if _, err := MergeSignedAssemblyInventory(base, []v2input.Entry{base[0], changed,
		entry(uuid.New(), "C2PAManifest")}); err == nil {
		t.Fatal("conflicting contract accepted")
	}
}
