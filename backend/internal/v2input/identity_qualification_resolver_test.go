package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2qualification"
)

type identityQualificationCatalogFixture struct {
	value  IdentityQualificationCandidate
	reads  int
	rotate bool
}

func (f *identityQualificationCatalogFixture) ResolveIdentityQualification(_ context.Context,
	_ IdentityQualificationQuery) (IdentityQualificationCandidate, error) {
	f.reads++
	value := f.value
	if f.rotate && f.reads > 1 {
		value.ReportObject.VersionID = "v2"
	}
	return value, nil
}

func TestResolveQualifiedIdentityPinReadsReportAndRechecksVersion(t *testing.T) {
	sha := func(letter string) string { return strings.Repeat(letter, 64) }
	fixturesRaw, err := os.ReadFile("../../../docs/implementation-v2/contracts/structural-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Fixtures map[string]json.RawMessage `json:"fixtures"`
	}
	if json.Unmarshal(fixturesRaw, &fixtures) != nil {
		t.Fatal("fixtures")
	}
	var calibration map[string]any
	if json.Unmarshal(fixtures.Fixtures["Calibration"], &calibration) != nil {
		t.Fatal("calibration")
	}
	calibration["qualified"] = true
	calibration["loss_weights"] = []float64{1. / 6, 1. / 6, 1. / 6, 1. / 6, 1. / 6, 1. / 6}
	calibrationRaw, err := canonical.Marshal(calibration)
	if err != nil {
		t.Fatal(err)
	}
	calibrationSum := sha256.Sum256(calibrationRaw)
	calibrationRef := Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(calibrationSum[:])}
	model := calibration["model_lock_sha"].(string)
	dataset := calibration["dataset_sha"].(string)
	issuer := uuid.New()
	report := v2qualification.Report{SchemaVersion: 1, Operation: "identity.compare",
		Version: "1", ModelLockSHA: model, RuntimeLockSHA: sha("b"),
		DeviceProfileSHA: sha("c"), DatasetSHA: dataset,
		InputFixtureSHAs: []string{sha("e")},
		Checks:           []v2qualification.Check{{ID: "quality", Passed: true, EvidenceSHA: sha("f")}},
		Metrics:          v2qualification.Metrics{P50MS: 10, P95MS: 20}, Status: "passed",
		IssuerID: issuer, CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
	reportRaw, err := canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	reportSum := sha256.Sum256(reportRaw)
	reportRef := Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(reportSum[:])}
	locks := v2qualification.DeploymentLocks{Operation: "identity.compare", Version: "1",
		Backend: "cpu", ModelLockSHA: model, RuntimeLockSHA: report.RuntimeLockSHA,
		DeviceProfileSHA: report.DeviceProfileSHA, DatasetSHA: dataset,
		IssuerID: issuer.String(), RequiredChecks: []string{"quality"},
		CalibrationSHAs: []string{calibrationRef.SHA256},
		TestFixtureSHAs: []string{sha("e")}, EvidenceSHAs: []string{sha("f")},
		LicenseCleared: true}
	expected, err := locks.Expected()
	if err != nil {
		t.Fatal(err)
	}
	pin := IdentityQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(),
		Report: reportRef, Calibration: calibrationRef, ModelLockSHA: model,
		RuntimeLockSHA: report.RuntimeLockSHA, DeviceProfileSHA: report.DeviceProfileSHA,
		DatasetSHA: dataset, Backend: "cpu"}
	catalog := &identityQualificationCatalogFixture{value: IdentityQualificationCandidate{
		Pin: pin, Expected: expected,
		ReportObject: objectstore.Object{Key: "report", VersionID: "v1",
			SHA256: reportRef.SHA256, Bytes: int64(len(reportRaw)), ContentType: "application/json"}}}
	query := IdentityQualificationQuery{Tenant: uuid.New(), Project: uuid.New(),
		Backend: "cpu", ModelLockSHA: model, Calibration: calibrationRef}
	got, err := ResolveQualifiedIdentityPin(context.Background(), query,
		calibrationRaw, catalog, sessionImageBytes(reportRaw))
	if err != nil || got != pin || catalog.reads != 2 {
		t.Fatalf("qualified pin rejected: %+v %v", got, err)
	}
	catalog.rotate = true
	catalog.reads = 0
	if _, err := ResolveQualifiedIdentityPin(context.Background(), query,
		calibrationRaw, catalog, sessionImageBytes(reportRaw)); err != ErrExpired {
		t.Fatalf("rotated report accepted: %v", err)
	}
}
