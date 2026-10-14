package v2draft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestBuildIdentityAdoptionInventoryBindsExactClosure(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	e := PreparedIdentityAdoption{verified: true, Scope: IdentityResultScope{Task: uuid.New(), Root: ref()}}
	e.Result.Report, e.Result.Mask, e.Result.Pixels = e.Scope.Root, ref(), ref()
	e.Calibration.Ref = ref()
	e.Inputs.Params.Calibration = e.Calibration.Ref
	for i, item := range []v2input.Ref{e.Result.Report, e.Result.Mask, e.Result.Pixels} {
		e.Result.Objects[i].SHA256 = item.SHA256
	}
	raw, digest, err := BuildIdentityAdoptionInventory(e)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("inventory digest mismatch")
	}
	var entries []DocumentRef
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	paths := []string{"$", "$/calibration", "$/changed_mask", "$/changed_mask/artifact"}
	if len(entries) != len(paths) {
		t.Fatal("incomplete identity closure")
	}
	for i, path := range paths {
		purpose := sha256.Sum256([]byte("tasks/" + e.Scope.Task.String() + "/" + path))
		if entries[i].Ordinal != i || entries[i].Purpose != hex.EncodeToString(purpose[:]) {
			t.Fatalf("wrong inventory occurrence %d", i)
		}
	}
	e.Result.Pixels = e.Result.Mask
	if _, _, err := BuildIdentityAdoptionInventory(e); err == nil {
		t.Fatal("duplicate artifact accepted")
	}
}
