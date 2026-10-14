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

func TestFrozenSeriesMaterialSelectsOnlyPinnedItem(t *testing.T) {
	evidence := seriesHoldoutFixture()
	otherID := uuid.New()
	params := frozenSeriesParams{
		SceneID: "collection:test", Stats: []v2input.Ref{
			evidence.SourceImage, evidence.StageImage,
		}, EVLimit: .35, GainMin: .95, GainMax: 1.05,
		MinConfidence: .75, Algorithm: "seriesmatch_v1",
	}
	identity := [3]float64{1, 1, 1}
	series := frozenSeriesCorrections{
		SceneID: params.SceneID, ReferenceItemID: otherID,
		Items: []FrozenSeriesItemCorrection{
			{ItemID: evidence.ItemID, RawGains: identity,
				AppliedGains: identity, Confidence: .9,
				Accepted: true, Reason: "applied"},
			{ItemID: otherID, RawGains: identity,
				AppliedGains: identity, Confidence: 1,
				Accepted: true, Reason: "reference"},
		}, Algorithm: "seriesmatch_v1",
	}
	encode := func(value any) ([]byte, string) {
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		return raw, hex.EncodeToString(digest[:])
	}
	paramsRaw, paramsSHA := encode(params)
	seriesRaw, seriesSHA := encode(series)
	evidence.Params.SHA256 = paramsSHA
	evidence.Series.SHA256 = seriesSHA
	selected, err := ValidateFrozenSeriesMaterial(context.Background(),
		evidence, paramsRaw, seriesRaw)
	if err != nil || selected.ItemID != evidence.ItemID {
		t.Fatalf("exact selected series item rejected: %v", err)
	}
	mutateSeries := func(change func(*frozenSeriesCorrections)) {
		t.Helper()
		altered := series
		altered.Items = append([]FrozenSeriesItemCorrection(nil), series.Items...)
		change(&altered)
		raw, sha := encode(altered)
		changed := evidence
		changed.Series.SHA256 = sha
		if _, err := ValidateFrozenSeriesMaterial(context.Background(),
			changed, paramsRaw, raw); err == nil {
			t.Fatal("invalid series correction accepted")
		}
	}
	mutateSeries(func(s *frozenSeriesCorrections) { s.SceneID = "foreign" })
	mutateSeries(func(s *frozenSeriesCorrections) { s.Items[1].ItemID = s.Items[0].ItemID })
	mutateSeries(func(s *frozenSeriesCorrections) { s.Items[0].AppliedEV = .36 })
	mutateSeries(func(s *frozenSeriesCorrections) {
		s.Items[0].Accepted = false
		s.Items[0].AppliedEV = .1
	})
	mutateSeries(func(s *frozenSeriesCorrections) { s.Items[0].ItemID = uuid.New() })
	if _, err := ValidateFrozenSeriesMaterial(context.Background(),
		evidence, paramsRaw, append([]byte(nil), seriesRaw...)); err != nil {
		t.Fatalf("immutable input bytes unexpectedly changed: %v", err)
	}
	badParams := params
	badParams.Stats = []v2input.Ref{evidence.SourceImage, evidence.SourceImage}
	badRaw, badSHA := encode(badParams)
	changed := evidence
	changed.Params.SHA256 = badSHA
	if _, err := ValidateFrozenSeriesMaterial(context.Background(),
		changed, badRaw, seriesRaw); err == nil {
		t.Fatal("duplicate stats source accepted")
	}
	if _, err := ValidateFrozenSeriesMaterial(context.Background(),
		evidence, paramsRaw, append(seriesRaw, ' ')); err == nil {
		t.Fatal("noncanonical series bytes accepted")
	}
}
