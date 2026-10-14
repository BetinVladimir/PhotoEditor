package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
)

type relightQualificationTestCatalog struct{ *donorQualificationTestCatalog }

func (c relightQualificationTestCatalog) ResolveRelightQualification(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, pin RelightQualificationPin) (string, objectstore.Object, objectstore.Object, error) {
	return c.ResolveDonorQualification(ctx, who, tenant, project, DonorQualificationPin(pin))
}
func TestRelightQualificationLoaderFencesObjectReads(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	pin := RelightQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), PolicySHA: strings.Repeat("a", 64), Report: ref(), Calibration: ref(), ModelLockSHA: strings.Repeat("a", 64), RuntimeLockSHA: strings.Repeat("a", 64), DeviceProfileSHA: strings.Repeat("a", 64), DatasetSHA: strings.Repeat("a", 64), IssuerID: uuid.New(), Backend: "cpu"}
	for _, mode := range []string{"denied", "null_version", "revoked_during_read", "changed_version_during_read"} {
		t.Run(mode, func(t *testing.T) {
			report := objectstore.Object{Key: "report", VersionID: "r1", SHA256: pin.Report.SHA256, Bytes: 2, ContentType: "application/json"}
			calibration := objectstore.Object{Key: "calibration", VersionID: "c1", SHA256: pin.Calibration.SHA256, Bytes: 2, ContentType: "application/json"}
			base := &donorQualificationTestCatalog{report: report, calibration: calibration}
			reader := &donorQualificationTestReader{report: []byte("{}"), calibration: []byte("{}")}
			expectedReads := 0
			switch mode {
			case "denied":
				base.denied = true
			case "null_version":
				base.report.VersionID = "null"
			case "revoked_during_read":
				expectedReads = 1
				reader.onOpen = func() { base.denied = true }
			case "changed_version_during_read":
				expectedReads = 1
				reader.onOpen = func() { base.report.VersionID = "r2" }
			}
			got, err := LoadRelightQualificationEvidence(context.Background(), relightQualificationTestCatalog{base}, reader, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), RelightParams{}, pin, v2qualification.Expected{})
			if err == nil || got.Report != nil || got.Calibration != nil || reader.calls != expectedReads {
				t.Fatal("unsafe evidence read", err, reader.calls)
			}
		})
	}
}
