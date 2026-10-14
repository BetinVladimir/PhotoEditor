package v2learning

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func seriesHoldoutFixture() FrozenSeriesHoldoutEvidence {
	ref := func(char string) v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat(char, 64)}
	}
	return FrozenSeriesHoldoutEvidence{
		SchemaVersion: 1, TenantID: uuid.New(), EventID: uuid.New(),
		AssetVersionID: uuid.New(), ApplyRunID: uuid.New(), ItemID: uuid.New(),
		ProducerTaskID: uuid.New(), SourceImage: ref("a"), StageImage: ref("b"),
		Params: ref("c"), Series: ref("d"),
	}
}

func TestFrozenSeriesHoldoutRequiresExactExpectedSource(t *testing.T) {
	value := seriesHoldoutFixture()
	raw, sha, err := CompileFrozenSeriesHoldoutEvidence(value)
	if err != nil {
		t.Fatal(err)
	}
	trainingRunID := uuid.New()
	ref, err := SeriesHoldoutRef(trainingRunID, value.EventID, sha)
	if err != nil {
		t.Fatal(err)
	}
	if ref.ArtifactID != uuid.NewSHA1(trainingRunID,
		[]byte("v23-series/"+value.EventID.String())) {
		t.Fatal("unstable series evidence identity")
	}
	if decoded, err := DecodeFrozenSeriesHoldoutEvidence(raw, ref, value); err != nil || decoded != value {
		t.Fatalf("exact evidence refused: %v", err)
	}
	foreign := value
	foreign.ItemID = uuid.New()
	if _, err := DecodeFrozenSeriesHoldoutEvidence(raw, ref, foreign); err == nil {
		t.Fatal("foreign item admitted")
	}
	foreign = value
	foreign.Series = foreign.Params
	if _, _, err := CompileFrozenSeriesHoldoutEvidence(foreign); err == nil {
		t.Fatal("aliased series source admitted")
	}
}
