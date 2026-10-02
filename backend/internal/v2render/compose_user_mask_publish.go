package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type CompositionUserMaskPublisher interface{ CompositionParamsPublisher }

// ValidateUserCompositionMask checks canonical MaskRef bytes against the exact
// source document SHA, canvas and complete transform chain. Source bytes must
// come from a scoped pinned read. Binary probabilities/lineage are not validated
// here and remain mandatory before task input registration.
func ValidateUserCompositionMask(raw, sourceRaw []byte, base v2input.SessionVersionBinding) (review.MaskRef, error) {
	var empty review.MaskRef
	if base.AssetVersionID == uuid.Nil || !validLayerArtifactRef(base.Source) || len(raw) < 2 || len(raw) > 1048576 || len(sourceRaw) < 2 || len(sourceRaw) > 8<<20 {
		return empty, v2input.ErrInvalid
	}
	jcs, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(jcs, raw) {
		return empty, v2input.ErrInvalid
	}
	sourceJCS, err := canonical.Bytes(sourceRaw)
	if err != nil || !bytes.Equal(sourceJCS, sourceRaw) {
		return empty, v2input.ErrInvalid
	}
	sum := sha256.Sum256(sourceRaw)
	if hex.EncodeToString(sum[:]) != base.Source.SHA256 {
		return empty, v2input.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 8 {
		return empty, v2input.ErrInvalid
	}
	for _, key := range []string{"artifact", "canvas", "transform_chain", "object_id", "selector", "confidence", "model_lock_id", "protected"} {
		if _, ok := fields[key]; !ok {
			return empty, v2input.ErrInvalid
		}
	}
	if string(fields["protected"]) != "true" && string(fields["protected"]) != "false" || string(fields["confidence"]) == "null" {
		return empty, v2input.ErrInvalid
	}
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil || source.DType != "float32" {
		return empty, v2input.ErrInvalid
	}
	canvas, err := canonical.Hash(source.Canvas)
	if err != nil || canvas != base.CanvasHash {
		return empty, v2input.ErrInvalid
	}
	mask, err := review.ParseMaskRef(raw)
	if err != nil || mask.Canvas != source.Canvas || !bytes.Equal(mask.TransformChain, source.TransformChain) {
		return empty, v2input.ErrInvalid
	}
	return mask, nil
}

// PublishUserCompositionMask persists only a typed, geometry-matched document.
// A ready document alone is not permission to execute its binary dependencies.
func PublishUserCompositionMask(ctx context.Context, who auth.Identity, tenant, document uuid.UUID, base v2input.SessionVersionBinding, raw, sourceRaw []byte, publisher CompositionUserMaskPublisher) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if publisher == nil || tenant == uuid.Nil || document == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if _, err := ValidateUserCompositionMask(raw, sourceRaw, base); err != nil {
		return v2input.Ref{}, err
	}
	ref, err := publisher.Publish(ctx, who, tenant, base.AssetVersionID, document, append([]byte(nil), raw...))
	if err != nil {
		return v2input.Ref{}, err
	}
	sum := sha256.Sum256(raw)
	if !validLayerArtifactRef(ref) || ref.SHA256 != hex.EncodeToString(sum[:]) {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	return ref, nil
}

// PublishVerifiedUserCompositionMask is the new-user-mask path. It reads the
// scoped pinned source, proves exact version/dimensions of the binary upload,
// checks all probabilities, then publishes the typed document. Atomic task
// admission must repeat authority; this preflight is not an execution token.
func PublishVerifiedUserCompositionMask(ctx context.Context, who auth.Identity, tenant, document uuid.UUID, base v2input.SessionVersionBinding, raw []byte, sources v2draft.ArtifactCatalog, pixels UserMaskPublicationCatalog, objects v2input.SessionAnalysisObjectReader, publisher CompositionUserMaskPublisher) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if sources == nil || pixels == nil || objects == nil || publisher == nil || tenant == uuid.Nil || document == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || base.AssetVersionID == uuid.Nil || !validLayerArtifactRef(base.Source) || len(raw) < 2 || len(raw) > 1048576 {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	// Own mutable request bytes across validation and publication.
	raw = append([]byte(nil), raw...)
	source, err := readCompositionJSON(ctx, who, tenant, v2draft.LayerRef{Ref: base.Source, Contract: "WorkingImage", Path: "UserMask.source"}, sources, objects, 8<<20)
	if err != nil {
		return v2input.Ref{}, err
	}
	mask, err := ValidateUserCompositionMask(raw, source, base)
	if err != nil {
		return v2input.Ref{}, err
	}
	width, height := mask.Canvas.Width, mask.Canvas.Height
	if width < 1 || width > 200000000 || height < 1 || height > 200000000/width {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	binary := v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}
	object, err := pixels.LookupUserMaskPixels(ctx, who, tenant, base.AssetVersionID, binary, width, height)
	if err != nil {
		return v2input.Ref{}, err
	}
	n := width * height * 4
	if object.SHA256 != binary.SHA256 || object.Bytes < n+10 || object.Bytes > n+65548 || object.ContentType != "application/x-npy" || object.Key == "" || object.VersionID == "" || object.VersionID == "null" {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	reader, err := objects.Open(ctx, object)
	if err != nil {
		return v2input.Ref{}, err
	}
	if reader == nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	verifyErr := VerifyUserMaskNPY(ctx, reader, object.Bytes, binary.SHA256, width, height)
	closeErr := reader.Close()
	if verifyErr != nil {
		return v2input.Ref{}, verifyErr
	}
	if closeErr != nil {
		return v2input.Ref{}, closeErr
	}
	ref, err := PublishUserCompositionMask(ctx, who, tenant, document, base, raw, source, publisher)
	if err != nil {
		return v2input.Ref{}, err
	}
	if err := pixels.BindUserMask(ctx, who, tenant, base, ref, raw); err != nil {
		return v2input.Ref{}, err
	}
	return ref, nil
}
