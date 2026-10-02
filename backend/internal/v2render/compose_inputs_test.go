package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestCompositionRootsDeclareParentsAndMasksSortedAndDeduplicated(t *testing.T) {
	layer, base, proposal := compositionInputs()
	compiled, err := CompileComposition(layer, base, &proposal, proposal, nil, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(compiled.JSON)
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	roots, err := CompositionInputRoots(compiled, params)
	if err != nil || len(roots) != 4 {
		t.Fatal(roots, err)
	}
	contracts := map[string]int{}
	for i, entry := range roots {
		contracts[entry.Contract]++
		if i > 0 && bytes.Compare(roots[i-1].Ref.ArtifactID[:], entry.Ref.ArtifactID[:]) >= 0 {
			t.Fatal("unsorted roots")
		}
	}
	if contracts["Params"] != 1 || contracts["WorkingImage"] != 1 || contracts["ImageResult"] != 1 || contracts["MaskRef"] != 1 {
		t.Fatal(contracts)
	}
	params.SHA256 = base.Source.SHA256
	if _, err := CompositionInputRoots(compiled, params); err == nil {
		t.Fatal("wrong params digest")
	}
	compiled.Params.SourceResult = &base.Source
	params.SHA256 = hex.EncodeToString(sum[:])
	if _, err := CompositionInputRoots(compiled, params); err == nil {
		t.Fatal("conflicting artifact contracts")
	}
}
