package v2learning

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type EvaluationCaseEvidence struct {
	EventID           uuid.UUID `json:"event_id"`
	ColorDelta        float64   `json:"color_delta"`
	ClippingDelta     float64   `json:"clipping_delta"`
	IdentityStatus    string    `json:"identity_status"`
	ProtectedMaxError float64   `json:"protected_max_error"`
}

type EvaluationEvidence struct {
	SchemaVersion         int                      `json:"schema_version"`
	CandidateSHA          string                   `json:"candidate_sha"`
	ManifestSHA           string                   `json:"manifest_sha"`
	HoldoutSessions       int                      `json:"holdout_sessions"`
	BaselineLoss          float64                  `json:"baseline_loss"`
	CandidateLoss         float64                  `json:"candidate_loss"`
	ColorP95Delta         float64                  `json:"color_p95_delta"`
	ClippingDeltaFraction float64                  `json:"clipping_delta_fraction"`
	SafetyRegression      bool                     `json:"safety_regression"`
	Eligible              bool                     `json:"eligible"`
	Reasons               []string                 `json:"reasons"`
	MetricEligible        bool                     `json:"metric_eligible"`
	SafetyStatus          string                   `json:"safety_status"`
	Cases                 []EvaluationCaseEvidence `json:"cases"`
}

func finiteBetween(value, lower, upper float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= lower && value <= upper
}

// VerifyEvaluationEvidence accepts only exact JCS bytes from the canonical
// evaluate output. It checks holdout membership and all gates derivable from
// the reported cases; model losses still require independent model replay.
func VerifyEvaluationEvidence(raw []byte, expectedSHA string,
	manifest TrainingManifest, manifestSHA, candidateSHA string) (EvaluationEvidence, error) {
	var evidence EvaluationEvidence
	if len(raw) < 2 || len(raw) > 8*1024*1024 || !validSHA(expectedSHA) ||
		!validSHA(manifestSHA) || !validSHA(candidateSHA) {
		return evidence, ErrInvalidTrainingDraft
	}
	frozenSHA, err := canonical.Hash(manifest)
	if err != nil || frozenSHA != manifestSHA {
		return evidence, ErrInvalidTrainingDraft
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, canonicalBody) {
		return evidence, ErrInvalidTrainingDraft
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expectedSHA {
		return evidence, ErrInvalidTrainingDraft
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil ||
		evidence.SchemaVersion != 1 || evidence.CandidateSHA != candidateSHA ||
		evidence.ManifestSHA != manifestSHA || manifest.SchemaVersion != 1 ||
		len(manifest.Examples) < 40 || len(evidence.Cases) < 10 ||
		len(evidence.Cases) > 5000 ||
		!finiteBetween(evidence.BaselineLoss, 0, 1e9) ||
		!finiteBetween(evidence.CandidateLoss, 0, 1e9) ||
		!finiteBetween(evidence.ColorP95Delta, -100, 100) ||
		!finiteBetween(evidence.ClippingDeltaFraction, -1, 1) {
		return EvaluationEvidence{}, ErrInvalidTrainingDraft
	}
	holdout := make([]uuid.UUID, 0, len(evidence.Cases))
	sessions := map[uuid.UUID]struct{}{}
	for _, item := range manifest.Examples {
		if item.Split == "holdout" {
			holdout = append(holdout, item.EventID)
			sessions[item.SessionID] = struct{}{}
		}
	}
	if len(holdout) != len(evidence.Cases) ||
		len(sessions) != evidence.HoldoutSessions || len(sessions) < 2 {
		return EvaluationEvidence{}, ErrInvalidTrainingDraft
	}
	color, clipping := math.Inf(-1), math.Inf(-1)
	safetyRegression, notMeasurable := false, false
	for index, item := range evidence.Cases {
		if item.EventID == uuid.Nil || item.EventID != holdout[index] ||
			!finiteBetween(item.ColorDelta, -100, 100) ||
			!finiteBetween(item.ClippingDelta, -1, 1) ||
			!finiteBetween(item.ProtectedMaxError, 0, 1e6) {
			return EvaluationEvidence{}, ErrInvalidTrainingDraft
		}
		switch item.IdentityStatus {
		case "pass", "no_person":
		case "fail":
			safetyRegression = true
		case "not_measurable":
			notMeasurable = true
		default:
			return EvaluationEvidence{}, ErrInvalidTrainingDraft
		}
		if item.ProtectedMaxError > 0 {
			safetyRegression = true
		}
		color = math.Max(color, item.ColorDelta)
		clipping = math.Max(clipping, item.ClippingDelta)
	}
	safetyStatus := "pass"
	if safetyRegression {
		safetyStatus = "fail"
	} else if notMeasurable {
		safetyStatus = "not_measurable"
	}
	metricEligible := evidence.BaselineLoss > 1e-12 &&
		evidence.CandidateLoss <= .9*evidence.BaselineLoss &&
		color <= 0 && clipping <= 0
	if evidence.ColorP95Delta != color ||
		evidence.ClippingDeltaFraction != clipping ||
		evidence.SafetyRegression != safetyRegression ||
		evidence.SafetyStatus != safetyStatus ||
		evidence.MetricEligible != metricEligible ||
		evidence.Eligible != (metricEligible && safetyStatus == "pass") {
		return EvaluationEvidence{}, ErrInvalidTrainingDraft
	}
	expectedReasons := make([]string, 0, 5)
	if evidence.BaselineLoss <= 1e-12 {
		expectedReasons = append(expectedReasons, "NO_IMPROVEMENT_POSSIBLE")
	} else if evidence.CandidateLoss > .9*evidence.BaselineLoss {
		expectedReasons = append(expectedReasons, "LOSS_REGRESSION")
	}
	if color > 0 {
		expectedReasons = append(expectedReasons, "COLOR_REGRESSION")
	}
	if clipping > 0 {
		expectedReasons = append(expectedReasons, "CLIPPING_REGRESSION")
	}
	if safetyStatus == "fail" {
		expectedReasons = append(expectedReasons, "SAFETY_REGRESSION")
	} else if safetyStatus == "not_measurable" {
		expectedReasons = append(expectedReasons, "SAFETY_NOT_MEASURABLE")
	}
	if len(expectedReasons) != len(evidence.Reasons) {
		return EvaluationEvidence{}, ErrInvalidTrainingDraft
	}
	for index, reason := range expectedReasons {
		if evidence.Reasons[index] != reason {
			return EvaluationEvidence{}, ErrInvalidTrainingDraft
		}
	}
	return evidence, nil
}
