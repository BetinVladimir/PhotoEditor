package review

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRemapDustProposalFramesReissuesSeriesIDs(t *testing.T) {
	raw, ref, base, oldID := dustFixture(t)
	parsed, err := ReadDustProposal(context.Background(), bytes.NewReader(raw), ref, base)
	if err != nil {
		t.Fatal(err)
	}
	mapped := map[ArtifactRef]ArtifactRef{}
	for i, frame := range parsed.document.Frames {
		mapped[frame] = ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat(string(rune('5'+i)), 64)}
	}
	callback := func(_ context.Context, frame ArtifactRef) (ArtifactRef, error) { return mapped[frame], nil }
	result, ids, err := RemapDustProposalFrames(context.Background(), raw, base, mapped[base], callback)
	if err != nil || len(ids) != 1 || ids[oldID] == oldID {
		t.Fatal("ordered series was not reissued", err)
	}
	reissued, err := ReadDustProposal(context.Background(), bytes.NewReader(result), ArtifactRef{ID: uuid.New(), SHA256: digestDustBytes(result)}, mapped[base])
	if err != nil || reissued.document.Candidates[0].ID != ids[oldID] {
		t.Fatal("reissued proposal is invalid", err)
	}
	if _, _, err := RemapDustProposalFrames(context.Background(), raw, base, mapped[base], func(_ context.Context, frame ArtifactRef) (ArtifactRef, error) {
		if frame != base {
			return mapped[base], nil
		}
		return mapped[frame], nil
	}); err == nil {
		t.Fatal("duplicate target frame accepted")
	}
}
