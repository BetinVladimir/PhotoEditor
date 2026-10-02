package v2learning

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type trainingDescriptorReader struct {
	pinned v2draft.PinnedFrameDescriptor
}

func (r trainingDescriptorReader) ReadFrameDescriptor(_ context.Context,
	_ auth.Identity, _, _ uuid.UUID,
	_ v2input.SessionVersionBinding, _ string,
	_ *v2draft.ResolvedCorrection) (v2draft.PinnedFrameDescriptor, error) {
	return r.pinned, nil
}

func TestTrainingDescriptorSourcePinsPreEditFeaturesAndFaces(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	frame := v2draft.FrameFeatures{Binding: binding, Faces: ref(),
		LumaQuantiles:    [5]float64{.1, .2, .3, .4, .5},
		ClippingFraction: .01, Sharpness: 1, PersonFraction: .2}
	reader := trainingDescriptorReader{pinned: v2draft.PinnedFrameDescriptor{
		AnalysisID: uuid.New(), Features: frame, ShardRef: ref(), Ordinal: 0}}
	source := TrainingDescriptorSource{Reader: reader}
	got, err := source.Load(context.Background(), auth.Identity{UserID: uuid.New()},
		uuid.New(), uuid.New(), binding, strings.Repeat("c", 64), nil)
	if err != nil || got.Faces != frame.Faces || got.Features[0] != .1 ||
		got.Features[10] != .5 || got.AnalysisID != reader.pinned.AnalysisID {
		t.Fatalf("descriptor evidence mismatch: %+v, %v", got, err)
	}
	reader.pinned.Features.Faces = v2input.Ref{}
	source.Reader = reader
	if _, err := source.Load(context.Background(), auth.Identity{}, uuid.New(),
		uuid.New(), binding, strings.Repeat("c", 64), nil); err == nil {
		t.Fatal("missing face evidence accepted")
	}
}
