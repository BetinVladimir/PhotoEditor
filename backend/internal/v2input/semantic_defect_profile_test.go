package v2input

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestSemanticDefectProfileCapacityBoundaries(t *testing.T) {
	costs := SemanticQualifiedCosts{RAMBytes: 401, ScratchBytes: 503}
	raw, err := CompileSemanticDefectProfile(6000, 4000, 512<<20, costs, 1<<40, 0, 1<<40, 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Backend  string                  `json:"backend"`
		Renderer string                  `json:"renderer_version"`
		Estimate sessionDescribeEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(raw, &profile) != nil || profile.Backend != "cpu" || profile.Renderer != "v2.2" {
		t.Fatal("invalid profile")
	}
	e := profile.Estimate
	if e.RAMBytes < (3<<30)+(512<<20)+e.OutputBytes+401 || e.ScratchBytes < (512<<20)+e.OutputBytes+503 || e.OutputBytes < 8*6000*4000 || e.PixelCount != 24000000 {
		t.Fatal("underestimated worker", e)
	}
	if _, err := CompileSemanticDefectProfile(6000, 4000, 512<<20, costs, e.RAMBytes, 0, e.ScratchBytes, e.OutputBytes); err != nil {
		t.Fatal("exact capacity", err)
	}
	for _, limits := range [][3]int64{{e.RAMBytes - 1, e.ScratchBytes, e.OutputBytes}, {e.RAMBytes, e.ScratchBytes - 1, e.OutputBytes}, {e.RAMBytes, e.ScratchBytes, e.OutputBytes - 1}} {
		if raw, err := CompileSemanticDefectProfile(6000, 4000, 512<<20, costs, limits[0], 0, limits[1], limits[2]); err != ErrInvalid || raw != nil {
			t.Fatal("insufficient capacity accepted", err)
		}
	}
	for _, shape := range [][2]int64{{7, 8}, {200000000, 2}, {9007199254740991, 9007199254740991}} {
		if _, err := CompileSemanticDefectProfile(shape[0], shape[1], 1, costs, 1<<40, 0, 1<<40, 1<<40); err != ErrInvalid {
			t.Fatal("unsafe dimensions", err)
		}
	}
	if _, err := CompileSemanticDefectProfile(8, 8, 9007199254740991, costs, 9007199254740991, 0, 9007199254740991, 1<<40); err != ErrInvalid {
		t.Fatal("overflow accepted", err)
	}
}

func TestSemanticDefectProfileExactDeclaredFiles(t *testing.T) {
	params := Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	rgb := Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	mask := Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	inputs := []Ref{rgb, mask}
	sizes := []SemanticInputSize{{mask, 256}, {params, 512}, {rgb, 768}}
	compile := func(in []Ref, metadata []SemanticInputSize) ([]byte, error) {
		return CompileSemanticDefectProfileFromInputs(8, 8, params, in, metadata, SemanticQualifiedCosts{}, 1<<40, 0, 1<<40, 1<<40)
	}
	got, err := compile(inputs, sizes)
	if err != nil {
		t.Fatal(err)
	}
	want, err := CompileSemanticDefectProfile(8, 8, 1536, SemanticQualifiedCosts{}, 1<<40, 0, 1<<40, 1<<40)
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
