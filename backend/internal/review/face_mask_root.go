package review

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type rasterizedFaceMask struct {
	Image        ArtifactRef    `json:"image"`
	Vectors      ArtifactRef    `json:"vectors"`
	Target       ProtectionMask `json:"target"`
	Protected    ProtectionMask `json:"protected"`
	TargetPNG    ArtifactRef    `json:"target_png"`
	ProtectedPNG ArtifactRef    `json:"protected_png"`
	Algorithm    string         `json:"algorithm"`
}

// VerifyFaceMaskRoot compares the complete declared mask against exact original
// producer bytes. The caller must derive the root from canonical catalog lineage
// and independently verify Params, upstream inputs, qualification and pixels.
// Manual targets live in RasterizedMask; skin masks have a standalone MaskRef.
func VerifyFaceMaskRoot(raw []byte, root ArtifactRef, expected ProtectionMask, source DefectSource) error {
	fail := func() error { return errors.New("invalid face mask root") }
	if !root.valid() || len(raw) < 2 || len(raw) > 1048576 || !validMaskBinding(expected) || expected.Protected || expected.Canvas != source.Canvas || !source.Image.valid() {
		return fail()
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != root.SHA256 {
		return fail()
	}
	sourceChain, err := canonical.Bytes(source.TransformChain)
	if err != nil {
		return fail()
	}
	matchGeometry := func(mask ProtectionMask) bool {
		chain, err := canonical.Bytes(mask.TransformChain)
		return err == nil && mask.Canvas == source.Canvas && bytes.Equal(chain, sourceChain)
	}
	if !matchGeometry(expected) {
		return fail()
	}
	var actual ProtectionMask
	switch expected.Selector {
	case "manual":
		var document rasterizedFaceMask
		if decodeExact(raw, &document) != nil || document.Algorithm != "manual_mask_v1" || document.Image != source.Image || !document.Vectors.valid() || !document.TargetPNG.valid() || !document.ProtectedPNG.valid() {
			return fail()
		}
		// Output roles are distinct artifacts even when all masks are empty.
		seen := map[uuid.UUID]bool{root.ID: true}
		for _, ref := range []ArtifactRef{document.Target.Artifact, document.Protected.Artifact, document.TargetPNG, document.ProtectedPNG} {
			if !ref.valid() || seen[ref.ID] {
				return fail()
			}
			seen[ref.ID] = true
		}
		actual = document.Target
		protected := document.Protected
		if !validMaskBinding(actual) || !validMaskBinding(protected) || actual.Selector != "manual" || protected.Selector != "manual" || actual.Protected || !protected.Protected || actual.Confidence != 1 || protected.Confidence != 1 || actual.ModelLockID != nil || protected.ModelLockID != nil || !matchGeometry(protected) || actual.Artifact.ID == protected.Artifact.ID {
			return fail()
		}
		if (actual.ObjectID == nil) != (protected.ObjectID == nil) || (actual.ObjectID != nil && *actual.ObjectID != *protected.ObjectID) {
			return fail()
		}
	case "skin":
		if decodeExact(raw, &actual) != nil || !validMaskBinding(actual) || actual.Selector != "skin" || actual.Protected {
			return fail()
		}
	default:
		return fail()
	}
	a, err := canonical.Marshal(actual)
	if err != nil {
		return fail()
	}
	b, err := canonical.Marshal(expected)
	if err != nil || !bytes.Equal(a, b) {
		return fail()
	}
	return nil
}

// ManualFaceMaskOutputs is the owned full output inventory of an original
// RasterizedMask. Catalog lineage and binary checks remain caller obligations.
type ManualFaceMaskOutputs struct {
	Target, Protected       ProtectionMask
	TargetPNG, ProtectedPNG ArtifactRef
}

func ResolveManualFaceMaskOutputs(raw []byte, root ArtifactRef, expected ProtectionMask, source DefectSource) (ManualFaceMaskOutputs, error) {
	empty := ManualFaceMaskOutputs{}
	if expected.Selector != "manual" {
		return empty, errors.New("manual face mask required")
	}
	if err := VerifyFaceMaskRoot(raw, root, expected, source); err != nil {
		return empty, err
	}
	var document rasterizedFaceMask
	if err := decodeExact(raw, &document); err != nil {
		return empty, err
	}
	return ManualFaceMaskOutputs{Target: document.Target, Protected: document.Protected, TargetPNG: document.TargetPNG, ProtectedPNG: document.ProtectedPNG}, nil
}

// ResolveOriginalManualTarget extracts the declaration from original pinned
// raster output bytes, then runs the complete source/output manifest validation.
// It does not attest producer origin or physical target/protected pixels.
func ResolveOriginalManualTarget(raw []byte, root, pixel ArtifactRef, source DefectSource) (ProtectionMask, error) {
	var document rasterizedFaceMask
	if !pixel.valid() || len(raw) < 2 || len(raw) > 1048576 || decodeExact(raw, &document) != nil || document.Target.Artifact != pixel {
		return ProtectionMask{}, errors.New("invalid original manual target")
	}
	outputs, err := ResolveManualFaceMaskOutputs(raw, root, document.Target, source)
	if err != nil {
		return ProtectionMask{}, err
	}
	return outputs.Target, nil
}

// ManualRasterDocument is declared original producer metadata. Parsing does not
// prove its source authority, Params selection or raster/vector equivalence.
type ManualRasterDocument struct {
	Image        ArtifactRef
	Vectors      ArtifactRef
	Target       ProtectionMask
	Protected    ProtectionMask
	TargetPNG    ArtifactRef
	ProtectedPNG ArtifactRef
}

func ParseManualRasterDocument(raw []byte, ref ArtifactRef) (ManualRasterDocument, error) {
	var zero ManualRasterDocument
	var root rasterizedFaceMask
	if len(raw) < 2 || len(raw) > 1048576 || decodeExact(raw, &root) != nil {
		return zero, errors.New("invalid original manual root")
	}
	source := DefectSource{Image: root.Image, Canvas: root.Target.Canvas, TransformChain: root.Target.TransformChain}
	if err := VerifyFaceMaskRoot(raw, ref, root.Target, source); err != nil {
		return zero, err
	}
	if err := validateTransformChain(root.Target.TransformChain, root.Target.Canvas); err != nil {
		return zero, err
	}
	return ManualRasterDocument{Image: root.Image, Vectors: root.Vectors, Target: root.Target, Protected: root.Protected, TargetPNG: root.TargetPNG, ProtectedPNG: root.ProtectedPNG}, nil
}
