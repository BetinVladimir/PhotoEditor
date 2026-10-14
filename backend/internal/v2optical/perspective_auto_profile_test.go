package v2optical

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"testing"
)

func TestPerspectiveLinesOriginalHashAndCanonicalContract(t *testing.T) {
	lines := PerspectiveLineSegments{SchemaVersion: 1, Lines: []PerspectiveLine{
		{A: PerspectivePoint{0, .2}, B: PerspectivePoint{1, .2}, Family: "horizontal"}, {A: PerspectivePoint{0, .8}, B: PerspectivePoint{1, .8}, Family: "horizontal"},
		{A: PerspectivePoint{.2, 0}, B: PerspectivePoint{.2, 1}, Family: "vertical"}, {A: PerspectivePoint{.8, 0}, B: PerspectivePoint{.8, 1}, Family: "vertical"}}}
	raw, _, err := lines.Compile()
	if err != nil {
		t.Fatal(err)
	}
	p := perspectiveIntent()
	p.Mode = "auto"
	p.Corners = nil
	ref := p.Base.Source
	ref.ArtifactID = uuid.New()
	sum := sha256.Sum256(raw)
	ref.SHA256 = hex.EncodeToString(sum[:])
	p.Lines = &ref
	got, err := VerifyPerspectiveLinesOriginal(p, raw)
	if err != nil || len(got.Lines) != 4 {
		t.Fatal(got, err)
	}
	changed := append([]byte(nil), raw...)
	changed[0] = ' '
	if _, err := VerifyPerspectiveLinesOriginal(p, changed); err == nil {
		t.Fatal("substitution accepted")
	}
	noncanonical := append([]byte(" "), raw...)
	sum = sha256.Sum256(noncanonical)
	p.Lines.SHA256 = hex.EncodeToString(sum[:])
	if _, err := VerifyPerspectiveLinesOriginal(p, noncanonical); err == nil {
		t.Fatal("noncanonical bytes accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompilePerspectiveAutoExecutionProfile(ctx, p, nil, raw, VerifiedPerspectiveQualification{}, 8000, PerspectiveResourceEstimate{}); err != context.Canceled {
		t.Fatal(err)
	}
}
