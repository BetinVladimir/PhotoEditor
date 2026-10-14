package review

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestManualMaskDocumentRefsBindCompleteMaskValues(t *testing.T) {
	raw, err := os.ReadFile("testdata/face-manual-root-python.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	rootRef := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	root, err := ParseManualRasterDocument(raw, rootRef)
	if err != nil {
		t.Fatal(err)
	}
	targetHash, _ := canonical.Hash(root.Target)
	guardHash, _ := canonical.Hash(root.Protected)
	target := ArtifactRef{ID: uuid.New(), SHA256: targetHash}
	guard := ArtifactRef{ID: uuid.New(), SHA256: guardHash}
	if err = VerifyManualMaskDocumentRefs(root, rootRef, target, guard); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []ArtifactRef{{ID: target.ID, SHA256: guardHash}, {ID: root.Target.Artifact.ID, SHA256: targetHash}, {ID: guard.ID, SHA256: targetHash}, {ID: rootRef.ID, SHA256: targetHash}, {ID: uuid.Nil, SHA256: targetHash}} {
		if VerifyManualMaskDocumentRefs(root, rootRef, bad, guard) == nil {
			t.Fatal("foreign standalone target accepted", bad)
		}
	}
	modified := root
	modified.Target.Confidence = .5
	if VerifyManualMaskDocumentRefs(modified, rootRef, target, guard) == nil {
		t.Fatal("modified target metadata accepted")
	}
	modified = root
	modified.Protected.Protected = false
	if VerifyManualMaskDocumentRefs(modified, rootRef, target, guard) == nil {
		t.Fatal("modified guard role accepted")
	}
	if VerifyManualMaskDocumentRefs(root, rootRef, guard, target) == nil {
		t.Fatal("swapped target and guard accepted")
	}
}
