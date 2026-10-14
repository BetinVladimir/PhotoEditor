package v2input

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestSemanticDetectorCapacityBoundaries(t *testing.T) {
	costs := SemanticQualifiedCosts{RAMBytes: 256 << 20, VRAMBytes: 2 << 30, ScratchBytes: 64 << 20}
	body, err := CompileSemanticDetectorProfile(6000, 4000, 300<<20, costs, 32<<30, 12<<30, 8<<30, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var profile sessionDescribeExecutionProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		t.Fatal(err)
	}
	e := profile.ResourceEstimate
	if profile.Backend != "cuda" || e.PixelCount != 24000000 || e.RAMBytes < (1<<30)+128*24000000+(300<<20)+costs.RAMBytes || e.VRAMBytes != costs.VRAMBytes || e.ScratchBytes < (300<<20)+costs.ScratchBytes {
		t.Fatal("missing input or inference residency")
	}
	limits := [4]int64{e.RAMBytes, e.VRAMBytes, e.ScratchBytes, e.OutputBytes}
	if _, err := CompileSemanticDetectorProfile(6000, 4000, 300<<20, costs, limits[0], limits[1], limits[2], limits[3]); err != nil {
		t.Fatal("exact boundary", err)
	}
	for i := range limits {
		denied := limits
		denied[i]--
		if body, err := CompileSemanticDetectorProfile(6000, 4000, 300<<20, costs, denied[0], denied[1], denied[2], denied[3]); err == nil || body != nil {
			t.Fatal("insufficient capacity accepted", i)
		}
	}
	for _, input := range []int64{0, -1, 9007199254740991} {
		if body, err := CompileSemanticDetectorProfile(6000, 4000, input, costs, 9007199254740991, 12<<30, 9007199254740991, 1<<20); err == nil || body != nil {
			t.Fatal("invalid/overflow input accepted")
		}
	}
	costs.VRAMBytes = 0
	if _, err := CompileSemanticDetectorProfile(6000, 4000, 1, costs, 32<<30, 12<<30, 8<<30, 1<<20); err == nil {
		t.Fatal("missing qualified GPU peak accepted")
	}
}

func TestSemanticDetectorExactDeclaredFiles(t *testing.T) {
	params := Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	rgb := Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	mask := Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	inputs := []Ref{rgb, mask}
	sizes := []SemanticInputSize{{mask, 256}, {params, 512}, {rgb, 768}}
	compile := func(in []Ref, metadata []SemanticInputSize) ([]byte, error) {
		return CompileSemanticDetectorProfileFromInputs(8, 8, params, in, metadata, SemanticQualifiedCosts{VRAMBytes: 1 << 30}, 1<<40, 12<<30, 1<<40, 1<<40)
	}
	got, err := compile(inputs, sizes)
	if err != nil {
		t.Fatal(err)
	}
	want, err := CompileSemanticDetectorProfile(8, 8, 1536, SemanticQualifiedCosts{VRAMBytes: 1 << 30}, 1<<40, 12<<30, 1<<40, 1<<40)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("incorrect file accounting", err)
	}
	reordered, err := compile([]Ref{mask, rgb}, sizes)
	if err != nil || !bytes.Equal(got, reordered) {
		t.Fatal("ordering changed profile", err)
	}
	for _, bad := range [][]SemanticInputSize{sizes[:2], {sizes[0], sizes[1], sizes[1]}, {sizes[0], sizes[1], {Ref{rgb.ArtifactID, strings.Repeat("d", 64)}, 768}}, {sizes[0], {params, 1048577}, sizes[2]}, {sizes[0], sizes[1], {rgb, 0}}} {
		raw, err := compile(inputs, bad)
		if err != ErrInvalid || raw != nil {
			t.Fatal("invalid metadata accepted", err)
		}
	}
	for _, bad := range [][]Ref{{rgb, rgb}, {params, mask}} {
		if raw, err := compile(bad, sizes); err != ErrInvalid || raw != nil {
			t.Fatal("duplicate declared file accepted", err)
		}
	}
}
