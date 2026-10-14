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
 estimate,err:=VerifiedTimingMilliseconds(body,digest,expected)
 if err!=nil||estimate==nil||*estimate!=20{t.Fatal("verified p95 estimate unavailable",estimate,err)}
 for _,sample:=range []struct{value float64;want int64}{{0,0},{0.1,1},{1000,1000},{1000.01,1001},{9007199254740991,9007199254740991}}{
  report.Metrics.P50MS=0;report.Metrics.P95MS=sample.value
  timedBody,timedSHA:=encode()
  measured,err:=VerifiedTimingMilliseconds(timedBody,timedSHA,expected)
  if err!=nil{t.Fatal(err)}
  if sample.value==0{if measured!=nil{t.Fatal("zero measurement became known timing")}}else if measured==nil||*measured!=sample.want{t.Fatal("incorrect timing rounding",measured)}
 }
 report.Metrics.P50MS=10;report.Metrics.P95MS=20
	expected.RuntimeLockSHA = sha("0")
	if _, err := Verify(body, digest, expected); err == nil {
		t.Fatal("wrong runtime accepted")
	}
 if measured,err:=VerifiedTimingMilliseconds(body,digest,expected);err==nil||measured!=nil{t.Fatal("unqualified timing accepted")}
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
