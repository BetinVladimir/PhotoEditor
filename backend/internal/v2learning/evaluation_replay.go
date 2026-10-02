package v2learning

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type lossCalibration struct {
	SchemaVersion       int       `json:"schema_version"`
	ModelLockSHA        string    `json:"model_lock_sha"`
	Qualified           bool      `json:"qualified"`
	NormalizationScales []float64 `json:"normalization_scales"`
	LossWeights         []float64 `json:"loss_weights"`
}

func verifiedEvaluationDocument(raw []byte, ref v2input.Ref) bool {
	if !validManifestRef(ref) || len(raw) < 2 || len(raw) > 8*1024*1024 {
		return false
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, canonicalBody) {
		return false
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]) == ref.SHA256
}

func replayPrediction(profile v2draft.CandidateProfile, features [12]float64) ([6]float64, error) {
	limits := [6]float64{1, .223144, .223144, .223144, .15, .15}
	var result [6]float64
	for j := range result {
		value := profile.Intercept[j]
		for i, feature := range features {
			if !finiteBetween(feature, -100, 100) {
				return result, ErrInvalidTrainingDraft
			}
			value += (feature - profile.Means[i]) / profile.Scales[i] * profile.Coefficients[i][j]
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return result, ErrInvalidTrainingDraft
		}
		result[j] = math.Min(limits[j], math.Max(-limits[j], value))
	}
	return result, nil
}

func replayLoss(profile v2draft.CandidateProfile, examples []ManifestExample,
	calibration lossCalibration) (float64, error) {
	var total float64
	for _, example := range examples {
		if example.Split != "holdout" {
			continue
		}
		prediction, err := replayPrediction(profile, example.Features)
		if err != nil {
			return 0, err
		}
		target := [6]float64{example.Target.EV, example.Target.LogR,
			example.Target.LogG, example.Target.LogB,
			example.Target.SaturationDelta, example.Target.RetouchDelta}
		var item float64
		for j, desired := range target {
			if math.IsNaN(desired) || math.IsInf(desired, 0) {
				return 0, ErrInvalidTrainingDraft
			}
			item += calibration.LossWeights[j] * math.Abs(prediction[j]-desired) /
				calibration.NormalizationScales[j]
		}
		total += item
	}
	count := 0
	for _, example := range examples {
		if example.Split == "holdout" {
			count++
		}
	}
	if count < 10 || math.IsNaN(total) || math.IsInf(total, 0) {
		return 0, ErrInvalidTrainingDraft
	}
	return total / float64(count), nil
}

// VerifyEvaluationLosses independently replays the frozen holdout using the
// exact artifact bytes pinned by the manifest. It rejects forged aggregate
// losses even when the Evaluation document itself has a valid object hash.
func VerifyEvaluationLosses(evaluation EvaluationEvidence, manifest TrainingManifest,
	manifestSHA string, candidateRef v2input.Ref, baselineBody, candidateBody,
	calibrationBody []byte) error {
	if !verifiedEvaluationDocument(baselineBody, manifest.Baseline) ||
		!verifiedEvaluationDocument(candidateBody, candidateRef) ||
		!verifiedEvaluationDocument(calibrationBody, manifest.Calibration) {
		return ErrInvalidTrainingDraft
	}
	baseline, err := v2draft.DecodeCandidateProfile(baselineBody)
	if err != nil {
		return ErrInvalidTrainingDraft
	}
	candidate, err := v2draft.DecodeCandidateProfile(candidateBody)
	if err != nil || candidate.ProfileID != manifest.ProfileID ||
		candidate.TrainingSHA != manifestSHA ||
		baseline.ProfileID != manifest.ProfileID {
		return ErrInvalidTrainingDraft
	}
	var calibration lossCalibration
	if json.Unmarshal(calibrationBody, &calibration) != nil ||
		calibration.SchemaVersion != 1 || !calibration.Qualified ||
		calibration.ModelLockSHA != manifest.ModelLockSHA ||
		len(calibration.LossWeights) != 6 || len(calibration.NormalizationScales) != 6 {
		return ErrInvalidTrainingDraft
	}
	var sum float64
	for i, weight := range calibration.LossWeights {
		if !finiteBetween(weight, 0, 1) ||
			!finiteBetween(calibration.NormalizationScales[i], 1e-6, 1e6) {
			return ErrInvalidTrainingDraft
		}
		sum += weight
	}
	if math.Abs(sum-1) > 1e-9 {
		return ErrInvalidTrainingDraft
	}
	baselineLoss, err := replayLoss(baseline, manifest.Examples, calibration)
	if err != nil {
		return err
	}
	candidateLoss, err := replayLoss(candidate, manifest.Examples, calibration)
	if err != nil || math.Abs(baselineLoss-evaluation.BaselineLoss) > 1e-9 ||
		math.Abs(candidateLoss-evaluation.CandidateLoss) > 1e-9 {
		return ErrInvalidTrainingDraft
	}
	return nil
}
