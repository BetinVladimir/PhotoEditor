package v2optical

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestDetectedLinesPreservesUnsupportedAndRejectsForeignVersion(t *testing.T) {
	version := uuid.New()
	value := DetectedPerspectiveLines{SchemaVersion: 1, Base: v2input.SessionVersionBinding{AssetVersionID: version, Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}, Status: "unsupported"}
	raw, err := canonical.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := canonical.Hash(value)
	if err != nil {
		t.Fatal(err)
	}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	parsed, err := ParseDetectedPerspectiveLines(raw, ref, version.String())
	if err != nil || parsed.Status != "unsupported" || parsed.Lines != nil {
		t.Fatal(err)
	}
	if _, err := ParseDetectedPerspectiveLines(raw, ref, uuid.New().String()); err == nil {
		t.Fatal("foreign version accepted")
	}
	if _, err := ParseDetectedPerspectiveLines(append(raw, ' '), ref, version.String()); err == nil {
		t.Fatal("noncanonical bytes accepted")
	}
	value.Status = "available"
	raw, _ = canonical.Marshal(value)
	ref.SHA256, _ = canonical.Hash(value)
	if _, err := ParseDetectedPerspectiveLines(raw, ref, version.String()); err == nil {
		t.Fatal("available without lines accepted")
	}
}
