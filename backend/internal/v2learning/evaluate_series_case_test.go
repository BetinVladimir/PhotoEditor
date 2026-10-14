package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestFrozenSeriesEvaluateCaseRequiresExactTrainingBinding(t *testing.T) {
	evidence := seriesHoldoutFixture()
	lease := LearningEvaluateLease{ExpansionID: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Hour), TenantID: evidence.TenantID,
		RunID: uuid.New(), WorkflowID: uuid.New(),
		Candidate: seriesStatsRefsFixture()[0]}
	_, sha, err := CompileFrozenSeriesHoldoutEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := SeriesHoldoutRef(lease.RunID, evidence.EventID, sha)
	if err != nil {
		t.Fatal(err)
	}
	caseRef := TrainingRenderCase{SeriesEvidence: &ref,
		Binding: v2input.SessionVersionBinding{
			AssetVersionID: evidence.AssetVersionID,
			Source:         evidence.SourceImage},
		WorkingImage: evidence.SourceImage}
	if err := ValidateFrozenSeriesCase(lease, evidence.EventID,
		caseRef, evidence); err != nil {
		t.Fatal("exact series case refused", err)
	}
	foreign := caseRef
	foreign.WorkingImage = evidence.StageImage
	if ValidateFrozenSeriesCase(lease, evidence.EventID,
		foreign, evidence) == nil {
		t.Fatal("foreign source accepted")
	}
	foreign = caseRef
	changed := ref
	changed.ArtifactID = uuid.New()
	foreign.SeriesEvidence = &changed
	if ValidateFrozenSeriesCase(lease, evidence.EventID,
		foreign, evidence) == nil {
		t.Fatal("foreign evidence ID accepted")
	}
	foreign = caseRef
	foreign.DetailEvidence = &v2input.Ref{
		ArtifactID: uuid.New(), SHA256: sha}
	if ValidateFrozenSeriesCase(lease, evidence.EventID,
		foreign, evidence) == nil {
		t.Fatal("combined unqualified effects accepted")
	}
}

func TestFrozenSeriesEvaluateInputsRequirePinnedAggregateAndContracts(t *testing.T) {
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
		Algorithm: "seriesmatch_v1"}
	otherStat := frozenSeriesStats{ItemID: other,
		Image:   v2input.Ref{ArtifactID: uuid.New(), SHA256: evidence.StageImage.SHA256},
		Regions: []frozenSeriesRegionStats{region}, Algorithm: "seriesmatch_v1"}
	selectedBody, selectedSHA := encode(selected)
	otherBody, otherSHA := encode(otherStat)
	statsRefs := []v2input.Ref{
		{ArtifactID: uuid.New(), SHA256: selectedSHA},
		{ArtifactID: uuid.New(), SHA256: otherSHA}}
	params := frozenSeriesParams{SceneID: "studio", Stats: statsRefs,
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
	paramsBody, paramsSHA := encode(params)
	seriesBody, seriesSHA := encode(series)
	evidence.Params.SHA256 = paramsSHA
	evidence.Series.SHA256 = seriesSHA
	_, evidenceSHA, err := CompileFrozenSeriesHoldoutEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	lease := LearningEvaluateLease{ExpansionID: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Hour), TenantID: evidence.TenantID,
		RunID: uuid.New(), WorkflowID: uuid.New(), Candidate: statsRefs[0]}
	ref, err := SeriesHoldoutRef(lease.RunID, evidence.EventID, evidenceSHA)
	if err != nil {
		t.Fatal(err)
	}
	caseRef := TrainingRenderCase{SeriesEvidence: &ref,
		Binding: v2input.SessionVersionBinding{
			AssetVersionID: evidence.AssetVersionID,
			Source:         evidence.SourceImage}, WorkingImage: evidence.SourceImage}
	proof := FrozenEvaluateSeriesInputs{Evidence: evidence,
		ParamsBody: paramsBody, SeriesBody: seriesBody,
		StatsBodies: [][]byte{selectedBody, otherBody},
		Entries: []v2input.Entry{
			{Ref: ref, Contract: "FrozenSeriesHoldoutEvidence"},
			{Ref: evidence.StageImage, Contract: "WorkingImage"},
			{Ref: evidence.Params, Contract: "SeriesParams"},
			{Ref: evidence.Series, Contract: "SeriesCorrections"},
			{Ref: statsRefs[0], Contract: "SeriesStats"},
			{Ref: statsRefs[1], Contract: "SeriesStats"}}}
	if err := validateFrozenSeriesEvaluateInputs(lease, evidence.EventID,
		caseRef, proof); err != nil {
		t.Fatal("valid frozen series closure refused", err)
	}
	changed := proof
	changed.Entries = append([]v2input.Entry(nil), proof.Entries...)
	changed.Entries[4].Contract = "WorkingImage"
	if validateFrozenSeriesEvaluateInputs(lease, evidence.EventID,
		caseRef, changed) == nil {
		t.Fatal("substituted Stats contract accepted")
	}
	changed = proof
	changed.StatsBodies = [][]byte{otherBody, selectedBody}
	if validateFrozenSeriesEvaluateInputs(lease, evidence.EventID,
		caseRef, changed) == nil {
		t.Fatal("reordered physical Stats accepted")
	}
}
