package v2learning

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestSeriesProducerCandidateRequiresExactEventAndSource(t *testing.T) {
	ref := func(digit string) v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat(digit, 64)}
	}
	tenantID, version, consent := uuid.New(), uuid.New(), uuid.New()
	image := ref("a")
	source := SeriesProducerCandidate{
		AssetVersionID: version, ConsentID: consent, ConsentEpoch: 2,
		ApplyRunID: uuid.New(), ItemID: uuid.New(), ProducerTaskID: uuid.New(),
		SourceImage: image, StageImage: ref("b"), Params: ref("c"), Series: ref("d"),
	}
	candidate := TrainingCandidate{
		EventID: uuid.New(), AssetVersionID: version, ConsentID: consent,
		ConsentEpoch: 2, SourceContentSHA: image.SHA256,
		ConsentLive: true, SourceReady: true, BaseReady: true, ApprovedReady: true,
	}
	render := TrainingRenderCase{
		WorkingImage: image,
		Binding: v2input.SessionVersionBinding{
			AssetVersionID: version, Source: image,
			CanvasHash: strings.Repeat("e", 64),
		},
	}
	if !validSeriesProducerCandidate(tenantID, source, candidate, render) {
		t.Fatal("exact current series source rejected")
	}
	for _, mutate := range []func(*SeriesProducerCandidate, *TrainingCandidate, *TrainingRenderCase){
		func(s *SeriesProducerCandidate, _ *TrainingCandidate, _ *TrainingRenderCase) { s.ItemID = uuid.Nil },
		func(s *SeriesProducerCandidate, _ *TrainingCandidate, _ *TrainingRenderCase) { s.ApplyRunID = uuid.Nil },
		func(s *SeriesProducerCandidate, _ *TrainingCandidate, _ *TrainingRenderCase) {
			s.ProducerTaskID = uuid.Nil
		},
		func(s *SeriesProducerCandidate, _ *TrainingCandidate, _ *TrainingRenderCase) {
			s.AssetVersionID = uuid.New()
		},
		func(s *SeriesProducerCandidate, _ *TrainingCandidate, _ *TrainingRenderCase) { s.ConsentEpoch++ },
		func(s *SeriesProducerCandidate, _ *TrainingCandidate, _ *TrainingRenderCase) {
			s.StageImage = s.SourceImage
		},
		func(s *SeriesProducerCandidate, _ *TrainingCandidate, _ *TrainingRenderCase) { s.Series = s.Params },
		func(s *SeriesProducerCandidate, _ *TrainingCandidate, _ *TrainingRenderCase) {
			s.Series.SHA256 = "not-a-sha"
		},
		func(_ *SeriesProducerCandidate, c *TrainingCandidate, _ *TrainingRenderCase) { c.ConsentLive = false },
		func(_ *SeriesProducerCandidate, c *TrainingCandidate, _ *TrainingRenderCase) { c.EventID = uuid.Nil },
		func(_ *SeriesProducerCandidate, _ *TrainingCandidate, r *TrainingRenderCase) {
			r.Binding.Source = ref("f")
		},
	} {
		s, c, r := source, candidate, render
		mutate(&s, &c, &r)
		if validSeriesProducerCandidate(tenantID, s, c, r) {
			t.Fatal("substituted series source admitted")
		}
	}
}
