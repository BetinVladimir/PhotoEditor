package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type RetouchPreviewBeauty struct {
	Enabled         bool    `json:"enabled"`
	Algorithm       string  `json:"algorithm"`
	Smoothing       float64 `json:"smoothing"`
	ToneEvenness    float64 `json:"tone_evenness"`
	TextureKeep     float64 `json:"texture_keep"`
	SkinMask        string  `json:"skin_mask"`
	ProtectFeatures bool    `json:"protect_features"`
}

type RetouchPreviewFace struct {
	Landmarks  v2input.Ref     `json:"landmarks"`
	ObjectID   *uuid.UUID      `json:"object_id"`
	Skin       json.RawMessage `json:"skin"`
	Manual     json.RawMessage `json:"manual"`
	ManualMode bool            `json:"manual_mode"`
}

// RetouchPreviewAdapterContext freezes content only. Landmark origin, source
// geometry, numerical arrays and qualification authority require separate gates.
type RetouchPreviewAdapterContext struct {
	SchemaVersion    int                           `json:"schema_version"`
	Algorithm        string                        `json:"algorithm"`
	Source           v2input.SessionVersionBinding `json:"source"`
	Candidate        v2input.Ref                   `json:"candidate"`
	TransferredMask  v2input.Ref                   `json:"transferred_mask"`
	Protection       v2input.Ref                   `json:"protection"`
	Faces            []RetouchPreviewFace          `json:"faces"`
	Beauty           RetouchPreviewBeauty          `json:"beauty"`
	ModelLockSHA     string                        `json:"model_lock_sha"`
	RuntimeLockSHA   string                        `json:"runtime_lock_sha"`
	DeviceProfileSHA string                        `json:"device_profile_sha"`
}

func validRetouchBeauty(b RetouchPreviewBeauty) bool {
	if !b.Enabled || b.Algorithm != "beautyfilter_v1" || b.SkinMask != "face68_conservative_v1" || !b.ProtectFeatures {
		return false
	}
	for _, value := range []float64{b.Smoothing, b.ToneEvenness, b.TextureKeep} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return false
		}
	}
	return true
}

// BindRetouchPreviewContext preserves exact canonical bytes and returns declared
// face dependencies, without granting task or canonical producer authority.
func BindRetouchPreviewContext(p RetouchPreviewParams, ref v2input.Ref, raw, transferredMask []byte) (RetouchPreviewAdapterContext, []v2input.Entry, error) {
	invalid := func() (RetouchPreviewAdapterContext, []v2input.Entry, error) {
		return RetouchPreviewAdapterContext{}, nil, ErrInvalid
	}
	if _, _, err := FreezeRetouchPreviewParams(p); err != nil {
		return invalid()
	}
	if ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(ref.SHA256) || len(raw) < 2 || len(raw) > 1048576 {
		return invalid()
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != ref.SHA256 {
		return invalid()
	}
	var c RetouchPreviewAdapterContext
	if decodeStrictWithLimit(raw, &c, 1048576) != nil {
		return invalid()
	}
	body, err := canonical.Marshal(c)
	if err != nil || !bytes.Equal(raw, body) || c.SchemaVersion != 1 || c.Algorithm != "beautyfilter_v1" || !validRetouchBeauty(c.Beauty) || len(c.Faces) < 1 || len(c.Faces) > 64 {
		return invalid()
	}
	if c.Source != p.Source || c.Candidate != p.Candidate || c.Protection != p.Protection || c.ModelLockSHA != p.ModelLockSHA || c.RuntimeLockSHA != p.RuntimeLockSHA || c.DeviceProfileSHA != p.DeviceProfileSHA {
		return invalid()
	}
	roots := map[uuid.UUID]bool{}
	for _, root := range []v2input.Ref{p.Plan, p.Intent, p.Source.Source, p.Candidate, p.Protection, c.TransferredMask, ref} {
		if root.ArtifactID == uuid.Nil || !shaPattern.MatchString(root.SHA256) || roots[root.ArtifactID] {
			return invalid()
		}
		roots[root.ArtifactID] = true
	}
	if len(transferredMask) < 2 || len(transferredMask) > 1048576 {
		return invalid()
	}
	maskSHA := sha256.Sum256(transferredMask)
	if hex.EncodeToString(maskSHA[:]) != c.TransferredMask.SHA256 {
		return invalid()
	}
	mask, err := review.ParseMaskRef(transferredMask)
	if err != nil || mask.Protected || mask.Selector != "manual" || roots[mask.Artifact.ID] {
		return invalid()
	}
	canonicalMask, err := canonical.Bytes(transferredMask)
	if err != nil || !bytes.Equal(canonicalMask, transferredMask) {
		return invalid()
	}
	entries := []v2input.Entry{{Ref: ref, Contract: "RetouchPreviewAdapterContext"}, {Ref: c.TransferredMask, Contract: "MaskRef"}}
	seen := map[string]bool{}
	landmarkContracts := map[uuid.UUID]string{}
	for _, face := range c.Faces {
		if !face.ManualMode || face.Landmarks.ArtifactID == uuid.Nil || !shaPattern.MatchString(face.Landmarks.SHA256) || roots[face.Landmarks.ArtifactID] || face.Landmarks.ArtifactID == mask.Artifact.ID || !bytes.Equal(face.Manual, transferredMask) {
			return invalid()
		}
		object := ""
		contract := "Face68"
		if face.ObjectID != nil {
			if *face.ObjectID == uuid.Nil {
				return invalid()
			}
			object = face.ObjectID.String()
			contract = "FaceLandmarks"
		}
		key := face.Landmarks.ArtifactID.String() + ":" + object
		if seen[key] {
			return invalid()
		}
		seen[key] = true
		if prior, ok := landmarkContracts[face.Landmarks.ArtifactID]; ok && prior != contract {
			return invalid()
		}
		landmarkContracts[face.Landmarks.ArtifactID] = contract
		entries = append(entries, v2input.Entry{Ref: face.Landmarks, Contract: contract}, v2input.Entry{Ref: v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}, Contract: "NpyArray"})
		if len(face.Skin) == 0 {
			return invalid()
		}
		if !bytes.Equal(face.Skin, []byte("null")) {
			skin, err := review.ParseMaskRef(face.Skin)
			if err != nil || skin.Protected || skin.Selector != "skin" || roots[skin.Artifact.ID] {
				return invalid()
			}
			entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: skin.Artifact.ID, SHA256: skin.Artifact.SHA256}, Contract: "NpyArray"})
		}
	}
	// One immutable artifact cannot acquire another content type or SHA through
	// a different face. Shared collections/masks remain legal when identical.
	declared := map[uuid.UUID]v2input.Entry{}
	for _, entry := range entries {
		if entry.Ref.ArtifactID == uuid.Nil {
			return invalid()
		}
		if prior, ok := declared[entry.Ref.ArtifactID]; ok &&
			(prior.Contract != entry.Contract || prior.Ref.SHA256 != entry.Ref.SHA256) {
			return invalid()
		}
		declared[entry.Ref.ArtifactID] = entry
	}
	return c, entries, nil
}
