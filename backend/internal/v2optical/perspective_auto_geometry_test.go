package v2optical

import (
	"context"
	"math"
	"testing"
)

func TestPerspectiveTranslatedBounds(t *testing.T) {
	identity := [3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	got, err := perspectiveTranslatedBounds(context.Background(), identity, 101, 79, 7979)
	if err != nil || got.Width != 101 || got.Height != 79 || got.SourceToOutput != identity {
		t.Fatal(got, err)
	}
	shifted := identity
	shifted[0][2] = -2
	shifted[1][2] = 3
	got, err = perspectiveTranslatedBounds(context.Background(), shifted, 101, 79, 7979)
	if err != nil || got.SourceToOutput != identity {
		t.Fatal(got, err)
	}
	for _, bad := range [][3][3]float64{{{1, 0, 0}, {0, 1, 0}, {-.02, 0, 1}}, {{math.NaN(), 0, 0}, {0, 1, 0}, {0, 0, 1}}} {
		if _, err := perspectiveTranslatedBounds(context.Background(), bad, 101, 79, 7979); err == nil {
			t.Fatal("invalid geometry accepted")
		}
	}
	if _, err := perspectiveTranslatedBounds(context.Background(), identity, 101, 79, 7978); err == nil {
		t.Fatal("cap ignored")
	}
}
func TestPerspectiveAutoProposalAxisAligned(t *testing.T) {
	lines := PerspectiveLineSegments{SchemaVersion: 1, Lines: []PerspectiveLine{
		{A: PerspectivePoint{0, .2}, B: PerspectivePoint{1, .2}, Family: "horizontal"},
		{A: PerspectivePoint{1, .8}, B: PerspectivePoint{0, .8}, Family: "horizontal"},
		{A: PerspectivePoint{.2, 0}, B: PerspectivePoint{.2, 1}, Family: "vertical"},
		{A: PerspectivePoint{.8, 1}, B: PerspectivePoint{.8, 0}, Family: "vertical"}}}
	got, err := CompilePerspectiveAutoProposal(context.Background(), lines, 100, 80, 8000)
	if err != nil || got.Width != 100 || got.Height != 80 {
		t.Fatal(got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompilePerspectiveAutoProposal(ctx, lines, 100, 80, 8000); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestPerspectiveAutoCoverageCenterSupport(t *testing.T) {
	p := PerspectiveAutoProposal{SourceToOutput: [3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}, Width: 4, Height: 3}
	fraction, err := PerspectiveAutoCoverage(context.Background(), p, 4, 3)
	if err != nil || fraction != 0 {
		t.Fatal(fraction, err)
	}
	p.Width = 5
	fraction, err = PerspectiveAutoCoverage(context.Background(), p, 4, 3)
	if err != nil || math.Abs(fraction-.2) > 1e-12 {
		t.Fatal(fraction, err)
	}
	p.SourceToOutput = [3][3]float64{}
	if _, err := PerspectiveAutoCoverage(context.Background(), p, 4, 3); err == nil {
		t.Fatal("singular matrix accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PerspectiveAutoCoverage(ctx, p, 4, 3); err != context.Canceled {
		t.Fatal(err)
	}
}
