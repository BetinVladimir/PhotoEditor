package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
)

func TestNativeFaceReportsPreserveAllFamiliesWithoutProducerProofs(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	reader := SQLTransferSourceReader{ProjectID: uuid.New(), Bucket: "owned"}
	tenant := uuid.New()
	sha := strings.Repeat("a", 64)
	proof := nativeFaceReportProof{tenant: tenant, project: reader.ProjectID, actor: who.UserID, session: who.SessionID, bucket: reader.Bucket}
	selected := map[string]v2qualification.Expected{}
	for i, op := range nativeFaceReportOperations {
		expected := v2qualification.Expected{Operation: op, Version: "1", Backend: "cpu", ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha}
		selected[op] = expected
		raw, err := canonical.Marshal(expected)
		if err != nil {
			t.Fatal(err)
		}
		proof.expected[i] = string(raw)
		proof.reports[i] = personEvidenceObject{contract: "QualificationReport", backend: "cpu", ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, object: objectstore.Object{Key: op, VersionID: "version-1", Bytes: 100, SHA256: sha, ContentType: "application/json"}}
	}
	proof.originalReports = proof.reports
	pins, err := reader.marshalNativeFaceReportPins(who, tenant, proof, selected)
	if err != nil || pins[0] == "" || pins[1] == "" || pins[2] == "" {
		t.Fatal("complete original report pins", err)
	}
	for _, mode := range []string{"parser-removed", "parser-role", "foreign-session", "foreign-bucket", "duplicate", "changed-sha", "missing-version", "alternate-qualified-report"} {
		changed := proof
		switch mode {
		case "alternate-qualified-report":
			changed.reports[2].ref.ArtifactID = uuid.New()
		case "parser-removed":
			changed.reports[2] = personEvidenceObject{}
		case "parser-role":
			changed.expected[2] = proof.expected[0]
		case "foreign-session":
			changed.session = uuid.New()
		case "foreign-bucket":
			changed.bucket = "foreign"
		case "duplicate":
			changed.reports[2].ref.ArtifactID = proof.reports[0].ref.ArtifactID
		case "changed-sha":
			changed.reports[2].object.SHA256 = strings.Repeat("b", 64)
		case "missing-version":
			changed.reports[2].object.VersionID = ""
		}
		if _, err := reader.marshalNativeFaceReportPins(who, tenant, changed, selected); err == nil {
			t.Fatal("invalid report evidence accepted", mode)
		}
	}
}
