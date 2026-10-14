package v2learning

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestReviewedDefectEventBindingRequiresExactVersionAndConsent(t *testing.T) {
	image := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	version, consent := uuid.New(), uuid.New()
	bound := ReviewedDefectEventBinding{ReviewID: uuid.New(),
		AssetVersionID: version, Source: image, ConsentID: consent,
		ConsentEpoch: 3}
	candidate := TrainingCandidate{EventID: uuid.New(), AssetVersionID: version,
		SourceContentSHA: image.SHA256, ConsentID: consent, ConsentEpoch: 3}
	render := TrainingRenderCase{WorkingImage: image,
		Binding: v2input.SessionVersionBinding{AssetVersionID: version,
			Source: image, CanvasHash: strings.Repeat("b", 64)}}
	if !validReviewedDefectEventBinding(bound, candidate, render) {
		t.Fatal("same reviewed source and event rejected")
	}
	for _, change := range []func(*ReviewedDefectEventBinding, *TrainingCandidate, *TrainingRenderCase){
		func(b *ReviewedDefectEventBinding, _ *TrainingCandidate, _ *TrainingRenderCase) {
			b.ReviewID = uuid.Nil
		},
		func(b *ReviewedDefectEventBinding, _ *TrainingCandidate, _ *TrainingRenderCase) {
			b.Source.ArtifactID = uuid.New()
		},
		func(b *ReviewedDefectEventBinding, _ *TrainingCandidate, _ *TrainingRenderCase) { b.ConsentEpoch++ },
		func(_ *ReviewedDefectEventBinding, c *TrainingCandidate, _ *TrainingRenderCase) {
			c.AssetVersionID = uuid.New()
		},
		func(_ *ReviewedDefectEventBinding, _ *TrainingCandidate, r *TrainingRenderCase) {
			r.Binding.Source.SHA256 = strings.Repeat("c", 64)
		},
	} {
		b, c, r := bound, candidate, render
		change(&b, &c, &r)
		if validReviewedDefectEventBinding(b, c, r) {
			t.Fatal("substituted event source accepted")
		}
	}
}
