package v2learning

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sort"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type detailMaterialParams struct {
	Image    v2input.Ref `json:"image"`
	Resolved v2input.Ref `json:"resolved"`
	Regions  []struct {
		RegionID  string          `json:"region_id"`
		Kind      string          `json:"kind"`
		Mask      json.RawMessage `json:"mask"`
		Confirmed bool            `json:"confirmed"`
		Strength  *float64        `json:"strength,omitempty"`
	} `json:"regions"`
	Protected []json.RawMessage `json:"protected"`
}

func exactDetailDocument(raw []byte, ref v2input.Ref) bool {
	if len(raw) < 2 || len(raw) > 1<<20 || !validManifestRef(ref) {
		return false
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != ref.SHA256 {
		return false
	}
	encoded, err := canonical.Bytes(raw)
	return err == nil && bytes.Equal(encoded, raw)
}

func sameDetailGeometry(mask review.MaskRef, image review.WorkingImage) bool {
	maskChain, err := canonical.Bytes(mask.TransformChain)
	if err != nil {
		return false
	}
	imageChain, err := canonical.Bytes(image.TransformChain)
	return err == nil && mask.Canvas == image.Canvas && bytes.Equal(maskChain, imageChain)
}

func detailResolvedEnabled(raw []byte, image v2input.Ref) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil || len(root) == 0 {
		return false
	}
	var savedHash string
	if json.Unmarshal(root["hash"], &savedHash) != nil || !validSHA(savedHash) {
		return false
	}
	delete(root, "hash")
	computed, err := canonical.Hash(root)
	if err != nil || computed != savedHash {
		return false
	}
	var params struct {
		Image v2input.Ref `json:"image"`
	}
	var mode string
	var modules, extensions map[string]json.RawMessage
	if json.Unmarshal(root["params"], &params) != nil || params.Image != image ||
		json.Unmarshal(root["mode"], &mode) != nil || mode == "skip" ||
		json.Unmarshal(root["modules"], &modules) != nil ||
		json.Unmarshal(modules["extensions"], &extensions) != nil ||
		modules == nil || extensions == nil {
		return false
	}
	enabled, err := portraitDetailModuleEnabled(extensions["portrait_detail"])
	return err == nil && enabled
}

// ValidatePortraitDetailProducerMaterial checks the physical JSON inputs of
// the linked producer and derives the complete binary-mask inventory. SQL
// authority over the training protection and every mask is still required.
func ValidatePortraitDetailProducerMaterial(source PortraitDetailProducerCandidate,
	protection v2input.Ref, imageRaw, paramsRaw, resolvedRaw, protectedRaw []byte,
) ([]v2input.Ref, error) {
	invalid := func() ([]v2input.Ref, error) { return nil, ErrInvalidTrainingDraft }
	if !exactDetailDocument(imageRaw, source.Image) ||
		!exactDetailDocument(paramsRaw, source.Params) ||
		!exactDetailDocument(resolvedRaw, source.Resolved) ||
		!exactDetailDocument(protectedRaw, protection) {
		return invalid()
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil || image.DType != "float32" ||
		image.Color.ProfileID != "work_rec2020_gamma24_v1" {
		return invalid()
	}
	guard, err := review.ParseMaskRef(protectedRaw)
	if err != nil || !guard.Protected || !sameDetailGeometry(guard, image) ||
		!detailResolvedEnabled(resolvedRaw, source.Image) {
		return invalid()
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(paramsRaw, &top) != nil || len(top) != 4 ||
		top["image"] == nil || top["resolved"] == nil ||
		top["regions"] == nil || top["protected"] == nil {
		return invalid()
	}
	var params detailMaterialParams
	decoder := json.NewDecoder(bytes.NewReader(paramsRaw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&params) != nil || decoder.Decode(new(any)) != io.EOF ||
		params.Image != source.Image || params.Resolved != source.Resolved ||
		len(params.Regions) < 1 || len(params.Regions) > 128 ||
		len(params.Protected) > 32 {
		return invalid()
	}
	refs := make([]v2input.Ref, 0, len(params.Regions)+len(params.Protected))
	ids := make(map[string]bool, len(params.Regions))
	for _, region := range params.Regions {
		if region.RegionID == "" || ids[region.RegionID] ||
			(region.Strength != nil && (*region.Strength < 0 || *region.Strength > 1)) {
			return invalid()
		}
		ids[region.RegionID] = true
		switch region.Kind {
		case "teeth", "sclera", "shine", "under_eye":
		case "cloth", "glasses":
			if !region.Confirmed {
				return invalid()
			}
		default:
			return invalid()
		}
		mask, err := review.ParseMaskRef(region.Mask)
		if err != nil || mask.Protected || !sameDetailGeometry(mask, image) {
			return invalid()
		}
		refs = append(refs, v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256})
	}
	foundGuard := false
	for _, raw := range params.Protected {
		mask, err := review.ParseMaskRef(raw)
		if err != nil || !mask.Protected || !sameDetailGeometry(mask, image) {
			return invalid()
		}
		if bytes.Equal(raw, protectedRaw) {
			foundGuard = true
		}
		refs = append(refs, v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256})
	}
	if !foundGuard || len(refs) > 160 {
		return invalid()
	}
	sort.Slice(refs, func(i, j int) bool {
		left, right := refs[i].ArtifactID, refs[j].ArtifactID
		return bytes.Compare(left[:], right[:]) < 0
	})
	for i, ref := range refs {
		if !validManifestRef(ref) || (i > 0 && refs[i-1].ArtifactID == ref.ArtifactID) {
			return invalid()
		}
	}
	return refs, nil
}
