package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// IdentityResultScope is issued from the canonical task/result row. The
// catalog must resolve only that task's registered input or output occurrence.
type IdentityResultScope struct {
	Tenant uuid.UUID
	Task   uuid.UUID
	Result uuid.UUID
	Root   v2input.Ref
}

type IdentityResultCatalog interface {
	ResolveIdentityResultObject(context.Context, IdentityResultScope, string, v2input.Ref) (objectstore.Object, error)
}

type VerifiedIdentityResult struct {
	Report  v2input.Ref
	Mask    v2input.Ref
	Pixels  v2input.Ref
	Status  string
	Objects [3]objectstore.Object
}

// VerifyIdentityPublishedResult checks the physical output trio against exact
// Params/after documents already authorized by the caller. It does not grant
// source authority or commit adoption; SQL must recheck these object versions.
func VerifyIdentityPublishedResult(ctx context.Context, scope IdentityResultScope,
	catalog IdentityResultCatalog, objects v2input.SessionAnalysisObjectReader,
	paramsRaw, afterRaw []byte) (VerifiedIdentityResult, error) {
	var zero VerifiedIdentityResult
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scope.Tenant == uuid.Nil || scope.Task == uuid.Nil || scope.Result == uuid.Nil ||
		!isDocumentRef(scope.Root) || catalog == nil || objects == nil {
		return zero, v2input.ErrInvalid
	}
	params, err := v2input.DecodeIdentityCompareParams(paramsRaw)
	if err != nil {
		return zero, err
	}
	if !identityPinnedAfter(params.After.Source, afterRaw) {
		return zero, v2input.ErrInvalid
	}
	after, err := review.ParseWorkingImage(afterRaw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	refs := [3]v2input.Ref{scope.Root}
	roles := [3]string{"report", "mask", "pixels"}
	var pinned [3]objectstore.Object
	readDocument := func(index int) ([]byte, error) {
		obj, err := catalog.ResolveIdentityResultObject(ctx, scope, roles[index], refs[index])
		if err != nil {
			return nil, err
		}
		if obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" ||
			obj.SHA256 != refs[index].SHA256 || obj.ContentType != "application/json" ||
			obj.Bytes < 2 || obj.Bytes > 8<<20 {
			return nil, v2input.ErrInvalid
		}
		raw, err := readJSONArtifact(ctx, objects, obj, refs[index])
		if err != nil {
			return nil, err
		}
		pinned[index] = obj
		return raw, nil
	}
	reportRaw, err := readDocument(0)
	if err != nil {
		return zero, err
	}
	if _, err = DocumentChildren(reportRaw, "IdentityReport", scope.Root); err != nil {
		return zero, err
	}
	var report struct {
		Status        string       `json:"status"`
		ChangedMask   v2input.Ref  `json:"changed_mask"`
		GeneratedMask *v2input.Ref `json:"generated_mask"`
		Calibration   v2input.Ref  `json:"calibration"`
	}
	if json.Unmarshal(reportRaw, &report) != nil || report.Calibration != params.Calibration ||
		report.GeneratedMask != nil || !isDocumentRef(report.ChangedMask) {
		return zero, v2input.ErrInvalid
	}
	refs[1] = report.ChangedMask
	maskRaw, err := readDocument(1)
	if err != nil {
		return zero, err
	}
	mask, err := review.ParseMaskRef(maskRaw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	afterChain, err := canonical.Bytes(after.TransformChain)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	maskChain, err := canonical.Bytes(mask.TransformChain)
	if err != nil || !bytes.Equal(afterChain, maskChain) ||
		mask.Canvas != after.Canvas || mask.Protected || mask.Selector != "identity_changed" ||
		mask.ObjectID != nil || mask.Confidence != 1 || mask.ModelLockID == nil ||
		*mask.ModelLockID != params.ModelLockSHA {
		return zero, v2input.ErrInvalid
	}
	refs[2] = v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}
	if !isDocumentRef(refs[2]) {
		return zero, v2input.ErrInvalid
	}
	pixel, err := catalog.ResolveIdentityResultObject(ctx, scope, roles[2], refs[2])
	if err != nil {
		return zero, err
	}
	if pixel.Key == "" || pixel.VersionID == "" || pixel.VersionID == "null" ||
		pixel.SHA256 != refs[2].SHA256 || pixel.ContentType != "application/x-npy" ||
		pixel.Bytes < 14 || pixel.Bytes > 800004096 {
		return zero, v2input.ErrInvalid
	}
	reader, err := objects.Open(ctx, pixel)
	if err != nil {
		return zero, err
	}
	if reader == nil {
		return zero, v2input.ErrInvalid
	}
	verifyErr := masknpy.VerifyBinaryMaskNPY(ctx, reader, pixel.Bytes, pixel.SHA256,
		after.Canvas.Width, after.Canvas.Height)
	closeErr := reader.Close()
	if verifyErr != nil {
		return zero, verifyErr
	}
	if closeErr != nil {
		return zero, closeErr
	}
	pinned[2] = pixel
	for i, ref := range refs {
		current, err := catalog.ResolveIdentityResultObject(ctx, scope, roles[i], ref)
		if err != nil {
			return zero, err
		}
		if current != pinned[i] {
			return zero, v2input.ErrExpired
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return VerifiedIdentityResult{Report: refs[0], Mask: refs[1], Pixels: refs[2],
		Status: report.Status, Objects: pinned}, nil
}

func identityPinnedAfter(ref v2input.Ref, raw []byte) bool {
	if !isDocumentRef(ref) {
		return false
	}
	_, err := review.ParseWorkingImage(raw)
	if err != nil {
		return false
	}
	canonicalRaw, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, canonicalRaw) {
		return false
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]) == ref.SHA256
}
