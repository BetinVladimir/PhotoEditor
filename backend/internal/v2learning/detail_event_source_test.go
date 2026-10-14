package v2learning

import (
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestPortraitDetailProducerCandidateBindsTrainingSourceAndStage(t *testing.T) {
	id := func() uuid.UUID { return uuid.New() }
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: id(),
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	}
	original, stage, params, resolved, protection := ref(), ref(), ref(), ref(), ref()
	version, consent := id(), id()
	candidate := TrainingCandidate{EventID: id(), AssetVersionID: version,
		SourceContentSHA: original.SHA256, ConsentID: consent, ConsentEpoch: 2,
		ConsentLive: true, SourceReady: true, BaseReady: true, ApprovedReady: true}
	render := TrainingRenderCase{
		Binding: v2input.SessionVersionBinding{AssetVersionID: version,
			Source: original}, WorkingImage: original, Protected: protection}
	source := PortraitDetailProducerCandidate{
		AssetVersionID: version, Source: original, ConsentID: consent,
		ConsentEpoch: 2, RunID: id(), TaskID: id(), ResultID: id(),
		AttemptID: id(), Image: stage, Params: params, Resolved: resolved}
	if !validPortraitDetailProducerCandidate(source, candidate, render) {
		t.Fatal("valid event-bound Apply producer refused")
	}
	for name, mutate := range map[string]func(*PortraitDetailProducerCandidate){
		"other source":         func(s *PortraitDetailProducerCandidate) { s.Source = ref() },
		"source used as stage": func(s *PortraitDetailProducerCandidate) { s.Image = original },
		"other consent":        func(s *PortraitDetailProducerCandidate) { s.ConsentID = id() },
		"other epoch":          func(s *PortraitDetailProducerCandidate) { s.ConsentEpoch++ },
		"missing result":       func(s *PortraitDetailProducerCandidate) { s.ResultID = uuid.Nil },
		"aliased params":       func(s *PortraitDetailProducerCandidate) { s.Params = stage },
	} {
		t.Run(name, func(t *testing.T) {
			changed := source
			mutate(&changed)
			if validPortraitDetailProducerCandidate(changed, candidate, render) {
				t.Fatal("changed producer admitted")
			}
		})
	}
}
