package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
)

func TestDetectorProofRetainsExactScopeAndDeployment(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project := uuid.New(), uuid.New()
	reader := SQLTransferSourceReader{ProjectID: project, Bucket: "bucket"}
	expected := v2qualification.Expected{Operation: "face.detect", Version: "1", Backend: "cuda", ModelLockSHA: strings.Repeat("a", 64)}
	body, err := canonical.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	proof := detectorQualificationProof{tenant: tenant, project: project, actor: who.UserID, session: who.SessionID, bucket: "bucket", expected: string(body), report: personEvidenceObject{ref: ref, contract: "QualificationReport", backend: "cuda", object: objectstore.Object{Key: "report", VersionID: "v1", SHA256: ref.SHA256, Bytes: 512}}}
	if _, err := reader.marshalDetectorQualificationProof(context.Background(), who, tenant, proof, expected); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"tenant", "actor", "session", "project", "bucket", "version", "hash", "deployment"} {
		changed := proof
		switch kind {
		case "tenant":
			changed.tenant = uuid.New()
		case "actor":
			changed.actor = uuid.New()
		case "session":
			changed.session = uuid.New()
		case "project":
			changed.project = uuid.New()
		case "bucket":
			changed.bucket = "other"
		case "version":
			changed.report.object.VersionID = "null"
		case "hash":
			changed.report.object.SHA256 = strings.Repeat("c", 64)
		case "deployment":
			other := expected
			other.ModelLockSHA = strings.Repeat("d", 64)
			raw, err := canonical.Marshal(other)
			if err != nil {
				t.Fatal(err)
			}
			changed.expected = string(raw)
		}
		if raw, err := reader.marshalDetectorQualificationProof(context.Background(), who, tenant, changed, expected); err == nil || raw != nil {
			t.Fatal("substituted proof accepted", kind)
		}
	}
	if err := reader.lockDetectorQualificationProof(context.Background(), nil, who, tenant, proof, expected); err == nil {
		t.Fatal("nil transaction accepted")
	}
}
