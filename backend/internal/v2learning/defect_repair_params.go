package v2learning

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// DecodeReviewedDefectRepairParams checks exact physical Params and image
// documents against a current SQL-scoped review source. Its protection result
// must still be reauthorized as a declared, ready repair-task input in SQL.
func DecodeReviewedDefectRepairParams(paramsRaw, imageRaw []byte,
	source ReviewedDefectSource) (review.MaskRef, v2input.Ref, error) {
	invalid := func() (review.MaskRef, v2input.Ref, error) {
		return review.MaskRef{}, v2input.Ref{}, ErrInvalidTrainingDraft
	}
	if len(paramsRaw) < 2 || len(paramsRaw) > 1<<20 ||
		len(imageRaw) < 2 || len(imageRaw) > 1<<20 ||
		!validManifestRef(source.Params) || !validManifestRef(source.Image) ||
		!validManifestRef(source.Proposal) || !validManifestRef(source.Selection) ||
		!validManifestRef(source.Resolved) {
		return invalid()
	}
	paramsDigest := sha256.Sum256(paramsRaw)
	imageDigest := sha256.Sum256(imageRaw)
	if hex.EncodeToString(paramsDigest[:]) != source.Params.SHA256 ||
		hex.EncodeToString(imageDigest[:]) != source.Image.SHA256 {
		return invalid()
	}
	var params struct {
		Image     review.ArtifactRef `json:"image"`
		Proposal  review.ArtifactRef `json:"proposal"`
		Review    review.ArtifactRef `json:"review"`
		Resolved  review.ArtifactRef `json:"resolved"`
		Protected json.RawMessage    `json:"protected"`
	}
	decoder := json.NewDecoder(bytes.NewReader(paramsRaw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&params) != nil || decoder.Decode(new(any)) != io.EOF ||
		len(params.Protected) < 2 {
		return invalid()
	}
	canonicalParams, err := canonical.Marshal(params)
	if err != nil || !bytes.Equal(canonicalParams, paramsRaw) ||
		params.Image.ID != source.Image.ArtifactID ||
		params.Image.SHA256 != source.Image.SHA256 ||
		params.Proposal.ID != source.Proposal.ArtifactID ||
		params.Proposal.SHA256 != source.Proposal.SHA256 ||
		params.Review.ID != source.Selection.ArtifactID ||
		params.Review.SHA256 != source.Selection.SHA256 ||
		params.Resolved.ID != source.Resolved.ArtifactID ||
		params.Resolved.SHA256 != source.Resolved.SHA256 {
		return invalid()
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil || image.DType != "float32" ||
		image.Color.ProfileID != "work_rec2020_gamma24_v1" {
		return invalid()
	}
	mask, err := review.ParseMaskRef(params.Protected)
	if err != nil || !mask.Protected || mask.Canvas != image.Canvas {
		return invalid()
	}
	maskChain, err := canonical.Bytes(mask.TransformChain)
	if err != nil {
		return invalid()
	}
	imageChain, err := canonical.Bytes(image.TransformChain)
	if err != nil || !bytes.Equal(maskChain, imageChain) {
		return invalid()
	}
	protection := v2input.Ref{ArtifactID: mask.Artifact.ID,
		SHA256: mask.Artifact.SHA256}
	if !validManifestRef(protection) {
		return invalid()
	}
	for _, other := range []v2input.Ref{source.Image, source.Proposal,
		source.Selection, source.Resolved, source.Params, source.LabelMap} {
		if protection.ArtifactID == other.ArtifactID {
			return invalid()
		}
	}
	return mask, protection, nil
}
