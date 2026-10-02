package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestEvaluationLossReplayRejectsForgedAggregate(t *testing.T) {
	_, profileID, _, manifest := compiledTrainingDraftFixture(t)
	sha := func(raw []byte) string {
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	encode := func(value any) []byte {
		body, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	profile := v2draft.CandidateProfile{SchemaVersion: 1,
		ProfileID: profileID, FeatureOrder: "scene12_v1", RidgeLambda: .1,
		TrainingSHA: strings.Repeat("0", 64)}
	for i := range profile.Scales {
		profile.Scales[i] = 1
	}
	baselineBody := encode(profile)
	manifest.Baseline.SHA256 = sha(baselineBody)
	calibration := struct {
		SchemaVersion       int       `json:"schema_version"`
		ModelLockSHA        string    `json:"model_lock_sha"`
		Qualified           bool      `json:"qualified"`
		NormalizationScales []float64 `json:"normalization_scales"`
		LossWeights         []float64 `json:"loss_weights"`
	}{1, manifest.ModelLockSHA, true, []float64{1, 1, 1, 1, 1, 1},
		[]float64{1, 0, 0, 0, 0, 0}}
	calibrationBody := encode(calibration)
	manifest.Calibration.SHA256 = sha(calibrationBody)
	manifestSHA, err := canonical.Hash(manifest)
	if err != nil {
		t.Fatal(err)
	}
	profile.TrainingSHA = manifestSHA
	profile.Intercept[0] = .25
	candidateBody := encode(profile)
	candidateRef := v2input.Ref{ArtifactID: manifest.Baseline.ArtifactID,
		SHA256: sha(candidateBody)}
	baseline, err := v2draft.DecodeCandidateProfile(baselineBody)
	if err != nil {
		t.Fatal(err)
	}
	baseLoss, err := replayLoss(baseline, manifest.Examples,
		lossCalibration{NormalizationScales: calibration.NormalizationScales,
			LossWeights: calibration.LossWeights})
	if err != nil {
		t.Fatal(err)
	}
	candidateLoss, err := replayLoss(profile, manifest.Examples,
		lossCalibration{NormalizationScales: calibration.NormalizationScales,
			LossWeights: calibration.LossWeights})
	if err != nil {
		t.Fatal(err)
	}
	evidence := EvaluationEvidence{BaselineLoss: baseLoss,
		CandidateLoss: candidateLoss}
	if err := VerifyEvaluationLosses(evidence, manifest, manifestSHA,
		candidateRef, baselineBody, candidateBody, calibrationBody); err != nil {
		t.Fatal(err)
	}
	evidence.CandidateLoss += .01
	if err := VerifyEvaluationLosses(evidence, manifest, manifestSHA,
		candidateRef, baselineBody, candidateBody, calibrationBody); err == nil {
		t.Fatal("forged loss accepted")
	}
	candidateBody[len(candidateBody)-1] = ' '
	if err := VerifyEvaluationLosses(EvaluationEvidence{BaselineLoss: baseLoss,
		CandidateLoss: candidateLoss}, manifest, manifestSHA,
		candidateRef, baselineBody, candidateBody, calibrationBody); err == nil {
		t.Fatal("modified candidate accepted")
	}
}
