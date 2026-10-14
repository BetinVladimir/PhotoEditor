package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
)

func TestPersonProofRetainsExactScopeAndDeployment(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project := uuid.New(), uuid.New()
	reader := SQLTransferSourceReader{ProjectID: project, Bucket: "bucket"}
	expected := v2qualification.Expected{Operation: "mask.person", Version: "1", Backend: "cuda", ModelLockSHA: strings.Repeat("a", 64)}
	body, err := canonical.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	proof := personQualificationProof{tenant: tenant, project: project, actor: who.UserID, session: who.SessionID, bucket: "bucket", expected: string(body), report: personEvidenceObject{ref: ref, contract: "QualificationReport", backend: "cuda", object: objectstore.Object{Key: "report", VersionID: "v1", SHA256: ref.SHA256, Bytes: 512}}}
	if _, err := reader.marshalPersonQualificationProof(context.Background(), who, tenant, proof, expected); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"photo_scheduler", "photo_api"} {
		tx := &manualLockTx{role: role}
		err := reader.lockPersonQualificationProof(context.Background(), tx, who, tenant, proof, expected)
		if role == "photo_api" {
			if !errors.Is(err, v2input.ErrInvalid) || len(tx.calls) != 1 {
				t.Fatal("wrong role advanced", err)
			}
			continue
		}
		pin, marshalErr := reader.marshalPersonQualificationProof(context.Background(), who, tenant, proof, expected)
		if err != nil || marshalErr != nil || len(tx.calls) != 2 || !strings.Contains(tx.calls[1], "lock_semantic_proposal_person_qualification") || tx.args[1][0] != tenant || tx.args[1][1] != project || tx.args[1][2] != who.UserID || tx.args[1][3] != who.SessionID || tx.args[1][4] != proof.expected || string(tx.args[1][5].([]byte)) != string(pin) {
			t.Fatal("person report lock pins changed", err)
		}
	}
	refused := &manualLockTx{role: "photo_scheduler", failAt: 2}
	if err := reader.lockPersonQualificationProof(context.Background(), refused, who, tenant, proof, expected); !errors.Is(err, v2input.ErrConflict) {
		t.Fatal("refused report lock advanced", err)
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
		if raw, err := reader.marshalPersonQualificationProof(context.Background(), who, tenant, changed, expected); err == nil || raw != nil {
			t.Fatal("substituted proof accepted", kind)
		}
	}
	if err := reader.lockPersonQualificationProof(context.Background(), nil, who, tenant, proof, expected); err == nil {
		t.Fatal("nil transaction accepted")
	}
}
