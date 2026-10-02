package v2qualification

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestVerifyQualificationBindsTrustedLocksAndEvidence(t *testing.T) {
	issuer := uuid.New()
	sha := func(letter string) string { return strings.Repeat(letter, 64) }
	report := Report{SchemaVersion: 1, Operation: "identity.compare",
		Version: "1", ModelLockSHA: sha("a"), RuntimeLockSHA: sha("b"),
		DeviceProfileSHA: sha("c"), DatasetSHA: sha("d"),
		InputFixtureSHAs: []string{sha("e")},
		Checks:           []Check{{ID: "quality", Passed: true, EvidenceSHA: sha("f")}},
		Metrics:          Metrics{P50MS: 10, P95MS: 20}, Status: "passed",
		IssuerID: issuer, CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
	expected := Expected{Operation: report.Operation, Version: report.Version,
		Backend: "cuda", ModelLockSHA: report.ModelLockSHA,
		RuntimeLockSHA:   report.RuntimeLockSHA,
		DeviceProfileSHA: report.DeviceProfileSHA, DatasetSHA: report.DatasetSHA,
		IssuerID: issuer, RequiredChecks: []string{"quality"},
		TestFixtureSHAs: map[string]struct{}{sha("e"): {}},
		EvidenceSHAs:    map[string]struct{}{sha("f"): {}},
		LicenseCleared:  true}
	encode := func() ([]byte, string) {
		body, err := canonical.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		return body, hex.EncodeToString(sum[:])
	}
	body, digest := encode()
	if _, err := Verify(body, digest, expected); err != nil {
		t.Fatal(err)
	}
	expected.RuntimeLockSHA = sha("0")
	if _, err := Verify(body, digest, expected); err == nil {
		t.Fatal("wrong runtime accepted")
	}
	expected.RuntimeLockSHA = report.RuntimeLockSHA
	expected.CalibrationSHAs = map[string]struct{}{sha("e"): {}}
	if _, err := Verify(body, digest, expected); err == nil {
		t.Fatal("calibration/test overlap accepted")
	}
	expected.CalibrationSHAs = nil
	report.Checks[0].Passed = false
	body, digest = encode()
	if _, err := Verify(body, digest, expected); err == nil {
		t.Fatal("failed check accepted")
	}
	report.Checks[0].Passed = true
	body, digest = encode()
	if _, err := Verify(body, sha("1"), expected); err == nil {
		t.Fatal("artifact SHA mismatch accepted")
	}
}
