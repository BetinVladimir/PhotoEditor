package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type dustSeriesSourceStub struct{ calls int }

func (s *dustSeriesSourceStub) QualifyImportRecipeSource(_ context.Context, _ uuid.UUID, _, _ v2input.SessionVersionBinding) error {
	s.calls++
	return nil
}

func TestBoundDustSeriesMapsOnlyAdoptedFramePair(t *testing.T) {
	ref := func(s string) v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte(s))} }
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref("old frame"), CanvasHash: rawSHA([]byte("canvas"))}
	newBinding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref("new frame"), CanvasHash: old.CanvasHash}
	otherOld := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref("other old"), CanvasHash: old.CanvasHash}
	otherNew := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref("other new"), CanvasHash: old.CanvasHash}
	frame := review.ArtifactRef{ID: otherOld.Source.ArtifactID, SHA256: otherOld.Source.SHA256}
	mapped := review.ArtifactRef{ID: otherNew.Source.ArtifactID, SHA256: otherNew.Source.SHA256}
	sources := &dustSeriesSourceStub{}
	authority := BoundImportDustSeriesAuthority{Entry: uuid.New(), Old: old, Target: newBinding, Bindings: []ImportCorrectionBinding{{Entry: uuid.New(), Old: old, New: newBinding}, {Entry: uuid.New(), Old: otherOld, New: otherNew}}, Sources: sources, Semantic: dustSeriesAuthorityStub{mapped: map[review.ArtifactRef]review.ArtifactRef{frame: mapped}}}
	got, err := authority.MapDustSeriesFrame(context.Background(), frame, old, newBinding)
	if err != nil || got != mapped || sources.calls != 1 {
		t.Fatal("adopted sibling frame rejected", err)
	}
	if _, err := authority.MapDustSeriesFrame(context.Background(), review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA([]byte("foreign"))}, old, newBinding); err == nil {
		t.Fatal("foreign frame accepted")
	}
	authority.Semantic = dustSeriesAuthorityStub{mapped: map[review.ArtifactRef]review.ArtifactRef{frame: {ID: uuid.New(), SHA256: mapped.SHA256}}}
	if _, err := authority.MapDustSeriesFrame(context.Background(), frame, old, newBinding); err == nil {
		t.Fatal("semantic redirect outside adopted pair accepted")
	}
}
