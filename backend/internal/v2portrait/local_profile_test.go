package v2portrait

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestLocalExecutionProfileSourceRuntimeAndCapacityBinding(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil {
		t.Fatal("fixture")
	}
	raw := images[0]
	source, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	canvas, _ := canonical.Hash(source.Canvas)
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, CanvasHash: canvas}
	cost, _ := LocalPortraitResources(source.Canvas.Width, source.Canvas.Height)
	profile, err := CompileLocalExecutionProfile(binding, raw, strings.Repeat("a", 64), cost)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Backend   string                `json:"backend"`
		Renderer  string                `json:"renderer_version"`
		Runtime   string                `json:"portrait_runtime_sha"`
		Resources LocalResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(profile, &parsed) != nil || parsed.Backend != "cpu" || parsed.Renderer != "v2.2" || parsed.Runtime != strings.Repeat("a", 64) || parsed.Resources != cost {
		t.Fatal("profile", string(profile))
	}
	for _, field := range []string{"ram", "scratch", "output", "pixels"} {
		limit := cost
		switch field {
		case "ram":
			limit.RAMBytes--
		case "scratch":
			limit.ScratchBytes--
		case "output":
			limit.OutputBytes--
		case "pixels":
			limit.PixelCount--
		}
		got, err := CompileLocalExecutionProfile(binding, raw, strings.Repeat("a", 64), limit)
		if err == nil || got != nil {
			t.Fatal("capacity accepted", field)
		}
	}
	for _, bad := range []string{"", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		got, err := CompileLocalExecutionProfile(binding, raw, bad, cost)
		if err == nil || got != nil {
			t.Fatal("unqualified pin shape", bad)
		}
	}
	got, err := CompileLocalExecutionProfile(binding, append(append([]byte{}, raw...), ' '), strings.Repeat("a", 64), cost)
	if err == nil || got != nil {
		t.Fatal("changed source accepted")
	}
	binding.CanvasHash = strings.Repeat("b", 64)
	got, err = CompileLocalExecutionProfile(binding, raw, strings.Repeat("a", 64), cost)
	if err == nil || got != nil {
		t.Fatal("foreign canvas accepted")
	}
}
