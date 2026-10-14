package review

import (
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// VerifyManualMaskDocumentRefs binds standalone canonical MaskRef hashes to
// exact declared target/protected values in a verified RasterizedMask. The
// producer publishes these documents using canonical_bytes. Catalog scope and
// physical reads remain caller obligations; artifact IDs cannot be inferred
// from document contents.
func VerifyManualMaskDocumentRefs(root ManualRasterDocument, rootRef, target, guard ArtifactRef) error {
	fail := func() error { return errors.New("invalid manual mask document refs") }
	if !rootRef.valid() || !target.valid() || !guard.valid() || target.ID == guard.ID {
		return fail()
	}
	used := map[uuid.UUID]bool{rootRef.ID: true, root.Image.ID: true, root.Vectors.ID: true,
		root.Target.Artifact.ID: true, root.Protected.Artifact.ID: true, root.TargetPNG.ID: true, root.ProtectedPNG.ID: true}
	if used[target.ID] || used[guard.ID] {
		return fail()
	}
	targetHash, err := canonical.Hash(root.Target)
	if err != nil || targetHash != target.SHA256 {
		return fail()
	}
	guardHash, err := canonical.Hash(root.Protected)
	if err != nil || guardHash != guard.SHA256 {
		return fail()
	}
	return nil
}
