package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"photoeditor/backend/internal/canonical"
)

func TestEvaluationEvidenceRequiresCanonicalHoldoutAndDerivedGates(t *testing.T) {
	_, _, _, manifest := compiledTrainingDraftFixture(t)
	manifestSHA, err := canonical.Hash(manifest)
	if err != nil {
		t.Fatal(err)
	}
	candidateSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	evidence := EvaluationEvidence{SchemaVersion: 1, CandidateSHA: candidateSHA,
		ManifestSHA: manifestSHA, BaselineLoss: 1, CandidateLoss: .8,
		ColorP95Delta: -.1, ClippingDeltaFraction: -.01,
		MetricEligible: true, Eligible: true, SafetyStatus: "pass",
		Reasons: []string{}, Cases: []EvaluationCaseEvidence{}}
	sessions := map[string]struct{}{}
	for _, example := range manifest.Examples {
		if example.Split != "holdout" {
			continue
		}
		sessions[example.SessionID.String()] = struct{}{}
		evidence.Cases = append(evidence.Cases, EvaluationCaseEvidence{
			EventID: example.EventID, ColorDelta: -.1, ClippingDelta: -.01,
			IdentityStatus: "pass", ProtectedMaxError: 0})
	}
	evidence.HoldoutSessions = len(sessions)
	encode := func(value EvaluationEvidence) ([]byte, string) {
		body, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		return body, hex.EncodeToString(sum[:])
	}
	body, digest := encode(evidence)
	if _, err := VerifyEvaluationEvidence(body, digest, manifest, manifestSHA, candidateSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyEvaluationEvidence(body, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", manifest, manifestSHA, candidateSHA); err == nil {
		t.Fatal("forged artifact SHA accepted")
	}
	evidence.Cases[0].ProtectedMaxError = .001
	body, digest = encode(evidence)
	if _, err := VerifyEvaluationEvidence(body, digest, manifest, manifestSHA, candidateSHA); err == nil {
		t.Fatal("false safety pass accepted")
	}
	evidence.Cases[0].ProtectedMaxError = 0
	evidence.Cases = evidence.Cases[:len(evidence.Cases)-1]
	body, digest = encode(evidence)
	if _, err := VerifyEvaluationEvidence(body, digest, manifest, manifestSHA, candidateSHA); err == nil {
		t.Fatal("missing holdout case accepted")
	}
}
