package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
	"time"
)

func TestPreparedDonorQualificationBinding(t *testing.T) {
	calibration := donorCalibrationFixture(t)
	runtime, device, fixture, evidence := strings.Repeat("c", 64), strings.Repeat("d", 64), strings.Repeat("e", 64), strings.Repeat("f", 64)
	issuer := uuid.New()
	locks := v2qualification.DeploymentLocks{Operation: "portrait.open_eyes", Version: "1", Backend: "cpu", ModelLockSHA: calibration.modelLockSHA, RuntimeLockSHA: runtime, DeviceProfileSHA: device, DatasetSHA: calibration.datasetSHA, IssuerID: issuer.String(), RequiredChecks: []string{"identity"}, CalibrationSHAs: []string{calibration.ref.SHA256}, TestFixtureSHAs: []string{fixture}, EvidenceSHAs: []string{evidence}, LicenseCleared: true}
	report := v2qualification.Report{SchemaVersion: 1, Operation: locks.Operation, Version: "1", ModelLockSHA: locks.ModelLockSHA, RuntimeLockSHA: runtime, DeviceProfileSHA: device, DatasetSHA: locks.DatasetSHA, InputFixtureSHAs: []string{fixture}, Checks: []v2qualification.Check{{ID: "identity", Passed: true, EvidenceSHA: evidence}}, Metrics: v2qualification.Metrics{P50MS: 1, P95MS: 2}, Status: "passed", IssuerID: issuer, CreatedAt: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)}
	raw, err := canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := canonical.Hash(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	pin := v2portrait.DonorQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: issuer, Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Calibration: calibration.ref, ModelLockSHA: locks.ModelLockSHA, RuntimeLockSHA: runtime, DeviceProfileSHA: device, DatasetSHA: locks.DatasetSHA, Backend: "cpu"}
	pin.PolicySHA, err = v2portrait.DonorPolicySHA(pin.PolicyID, locks)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := v2portrait.VerifyDonorQualificationEvidence(pin, locks, raw, calibration.raw)
	if err != nil {
		t.Fatal(err)
	}
	located, err := v2portrait.BindDonorQualificationObjects(verified, "test", objectstore.Object{Key: "report", VersionID: "1", SHA256: sha, Bytes: int64(len(raw)), ContentType: "application/json"}, objectstore.Object{Key: "calibration", VersionID: "1", SHA256: calibration.ref.SHA256, Bytes: int64(len(calibration.raw)), ContentType: "application/json"})
	if err != nil {
		t.Fatal(err)
	}
	loaded := LoadedDonorTaskPlan{runtimeSHA: runtime}
	prepared := PreparedDonorInputEvidence{calibration: &calibration, qualification: &located, Faces: LoadedDonorFaceEvidence{minEdge: 64}}
	if got, err := verifyPreparedDonorQualification(loaded, prepared); err != nil || got != pin {
		t.Fatal("qualified binding", err)
	}
	for _, mode := range []string{"runtime", "threshold", "calibration", "qualification", "bytes"} {
		l, p, c := loaded, prepared, calibration
		c.raw = append([]byte(nil), calibration.raw...)
		p.calibration = &c
		switch mode {
		case "runtime":
			l.runtimeSHA = fixture
		case "threshold":
			p.Faces.minEdge = 32
		case "calibration":
			p.calibration = nil
		case "qualification":
			p.qualification = nil
		case "bytes":
			c.raw[0] ^= 1
		}
		if _, err := verifyPreparedDonorQualification(l, p); err == nil {
			t.Fatal("accepted changed binding", mode)
		}
		if frozen, err := FreezeQualifiedDonorTask(context.Background(), uuid.New(), 0, l, p, v2portrait.PublishedDonorTaskPlan{}); err == nil || frozen.entries != nil {
			t.Fatal("invalid qualified task escaped", mode)
		}
	}
	catalog := &donorCompositionCatalog{}
	resolver := ClosureResolver{Catalog: catalog}
	loaded.runtimeSHA = fixture
	if got, err := resolver.PrepareQualifiedDonorInputEvidence(context.Background(), auth.Identity{}, uuid.New(), loaded, catalog, DonorManualProducer{}, located); err == nil || got.Entries != nil || catalog.calls != 0 {
		t.Fatal("runtime mismatch performed IO")
	}
}
