package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestFrozenSeriesStatsRequireEveryPinnedItemAndExactCommonRegions(t *testing.T) {
	evidence := seriesHoldoutFixture()
	other := uuid.New()
	encode := func(value any) ([]byte, string) {
		t.Helper()
		body, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		return body, hex.EncodeToString(sum[:])
	}
	region := frozenSeriesRegionStats{RegionID: "sky",
		MeanRGB: [3]float64{.5, .5, .5}, MeanY: .5,
		Coverage: .4, Confidence: .9}
	selected := frozenSeriesStats{ItemID: evidence.ItemID,
		Image: evidence.StageImage, Regions: []frozenSeriesRegionStats{region},
		ClippingFraction: .01, Algorithm: "seriesmatch_v1"}
	otherStat := frozenSeriesStats{ItemID: other,
		Image:   v2input.Ref{ArtifactID: uuid.New(), SHA256: evidence.StageImage.SHA256},
		Regions: []frozenSeriesRegionStats{region}, Algorithm: "seriesmatch_v1"}
	selectedRaw, selectedSHA := encode(selected)
	otherRaw, otherSHA := encode(otherStat)
	params := frozenSeriesParams{SceneID: "studio", Stats: []v2input.Ref{
		{ArtifactID: uuid.New(), SHA256: selectedSHA},
		{ArtifactID: uuid.New(), SHA256: otherSHA}},
		EVLimit: .35, GainMin: .95, GainMax: 1.05,
		MinConfidence: .75, Algorithm: "seriesmatch_v1"}
	identity := [3]float64{1, 1, 1}
	series := frozenSeriesCorrections{SceneID: "studio", ReferenceItemID: other,
		CommonRegionIDs: []string{"sky"}, Algorithm: "seriesmatch_v1",
		Items: []FrozenSeriesItemCorrection{
			{ItemID: evidence.ItemID, RawGains: identity,
				AppliedGains: identity, Confidence: .9, Accepted: true,
				Reason: "applied"},
			{ItemID: other, RawGains: identity,
				AppliedGains: identity, Confidence: 1, Accepted: true,
				Reason: "reference"}}}
	paramsRaw, paramsSHA := encode(params)
	seriesRaw, seriesSHA := encode(series)
	evidence.Params.SHA256 = paramsSHA
	evidence.Series.SHA256 = seriesSHA
	check := func(bodies [][]byte) error {
		return ValidateFrozenSeriesStats(context.Background(), evidence,
			paramsRaw, seriesRaw, bodies)
	}
	if err := check([][]byte{selectedRaw, otherRaw}); err != nil {
		t.Fatalf("valid aggregate refused: %v", err)
	}
	if err := check([][]byte{selectedRaw}); err == nil {
		t.Fatal("missing stat accepted")
	}
	if err := check([][]byte{otherRaw, selectedRaw}); err == nil {
		t.Fatal("stats reordered against pinned refs accepted")
	}
	badSelected := selected
	badSelected.ItemID = uuid.New()
	badRaw, badSHA := encode(badSelected)
	badParams := params
	badParams.Stats = append([]v2input.Ref(nil), params.Stats...)
	badParams.Stats[0].SHA256 = badSHA
	badParamsRaw, badParamsSHA := encode(badParams)
	badEvidence := evidence
	badEvidence.Params.SHA256 = badParamsSHA
	if err := ValidateFrozenSeriesStats(context.Background(), badEvidence,
		badParamsRaw, seriesRaw, [][]byte{badRaw, otherRaw}); err == nil {
		t.Fatal("foreign item accepted")
	}
	badImage := selected
	badImage.Image = otherStat.Image
	badRaw, badSHA = encode(badImage)
	badParams.Stats[0].SHA256 = badSHA
	badParamsRaw, badParamsSHA = encode(badParams)
	badEvidence.Params.SHA256 = badParamsSHA
	if err := ValidateFrozenSeriesStats(context.Background(), badEvidence,
		badParamsRaw, seriesRaw, [][]byte{badRaw, otherRaw}); err == nil {
		t.Fatal("selected item with foreign stage image accepted")
	}
	wrongCommon := series
	wrongCommon.CommonRegionIDs = nil
	wrongSeriesRaw, wrongSeriesSHA := encode(wrongCommon)
	badEvidence = evidence
	badEvidence.Series.SHA256 = wrongSeriesSHA
	if err := ValidateFrozenSeriesStats(context.Background(), badEvidence,
		paramsRaw, wrongSeriesRaw, [][]byte{selectedRaw, otherRaw}); err == nil {
		t.Fatal("incorrect common regions accepted")
	}
}
