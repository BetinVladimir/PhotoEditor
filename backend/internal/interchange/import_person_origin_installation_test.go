package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/workflow"
)

func TestImportPersonOriginInstallationOwnsExactIssuerKeys(t *testing.T) {
	_, scope, keys := portablePersonOriginFixture(t)
	installed, err := NewImportPersonOriginRecorderInstallation(scope.ModelLockSHA, keys)
	if err != nil || len(installed.trusted) != 1 {
		t.Fatal("trusted issuer installation refused", err)
	}
	for key, public := range keys {
		public[0] ^= 0xff
		delete(keys, key)
	}
	if len(installed.trusted) != 1 {
		t.Fatal("caller mutated installed trust")
	}
	if _, err := NewImportPersonOriginRecorderInstallation(scope.ModelLockSHA, keys); err == nil {
		t.Fatal("empty issuer trust accepted")
	}
}

func TestPinnedImportCandidateParsedSourceRequiresCurrentScope(t *testing.T) {
	source := importCandidateParsedSource{candidate: workflow.InterchangeImportCandidate{
		Tenant: uuid.New(), Workflow: uuid.New(), Run: uuid.New(), Artifact: uuid.New(),
		SHA: rawSHA([]byte("parsed")), Bytes: 10, Bucket: "archive", Key: "parsed", Version: "v1"}}
	ref, object, err := source.ResolveReviewParsed(context.Background())
	if err != nil || ref.ArtifactID != source.candidate.Artifact || object.VersionID != "v1" {
		t.Fatal(err)
	}
	source.candidate.Version = "null"
	if _, _, err := source.ResolveReviewParsed(context.Background()); err == nil {
		t.Fatal("unversioned parsed source accepted")
	}
}
