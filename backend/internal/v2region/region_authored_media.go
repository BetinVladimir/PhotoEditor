package v2region

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

const AuthoredRegionDerivativeMaxBytes = 16 << 20
const AuthoredRegionManifestMaxBytes = 1 << 20

// Proof binds a physically verified/adopted crop to the current authorized
// request. A ready task alone is insufficient. Recheck must read current SQL
// authority after object IO and immediately before a response is committed.
type AuthoredRegionMediaProof struct {
	Tenant, Asset, Actor, Session uuid.UUID
	Revision                      int64
	Canonical                     AuthoredCropVerification
}

type AuthoredRegionMediaDescriptor struct {
	SchemaVersion int         `json:"schema_version"`
	Request       uuid.UUID   `json:"request_id"`
	Revision      int64       `json:"revision,string"`
	IntentSHA     string      `json:"intent_sha256"`
	Manifest      v2input.Ref `json:"manifest"`
	// JSON encodes []byte as base64, preserving the exact hashed document bytes.
	ManifestBytes  []byte      `json:"manifest_base64"`
	Derivative     v2input.Ref `json:"derivative"`
	Bytes          int64       `json:"bytes"`
	MediaType      string      `json:"media_type"`
	ReviewRequired bool        `json:"review_required"`
}

// Media contains only the scoped derivative, never a master object locator.
// The concrete reader independently verifies the typed manifest, physical
// reconstruction, adoption receipt and current canonical result before return.
type AuthoredRegionMedia struct {
	Proof      AuthoredRegionMediaProof
	Descriptor AuthoredRegionMediaDescriptor
	Body       []byte
}

// Freeze copies bounded buffers before checking them, preventing the response
// from sharing mutable buffers with an object reader or codec pool.
func FreezeAuthoredRegionMedia(m AuthoredRegionMedia) (AuthoredRegionMedia, error) {
	invalid := func() (AuthoredRegionMedia, error) { return AuthoredRegionMedia{}, ErrRegionInvalid }
	d := m.Descriptor
	p := m.Proof
	c := p.Canonical
	if len(m.Body) == 0 || len(m.Body) > AuthoredRegionDerivativeMaxBytes || len(d.ManifestBytes) == 0 || len(d.ManifestBytes) > AuthoredRegionManifestMaxBytes {
		return invalid()
	}
	if p.Tenant == uuid.Nil || p.Asset == uuid.Nil || p.Actor == uuid.Nil || p.Session == uuid.Nil || p.Revision < 1 {
		return invalid()
	}
	if c.Request == uuid.Nil || c.Run == uuid.Nil || c.Task == uuid.Nil || c.Result == uuid.Nil || c.Root == uuid.Nil || c.Attempt < 1 || c.Fence < 1 {
		return invalid()
	}
	if d.SchemaVersion != 1 || d.Request != c.Request || d.Revision != p.Revision || d.IntentSHA != c.IntentSHA || d.Bytes != int64(len(m.Body)) {
		return invalid()
	}
	validSHA := func(s string) bool {
		b, e := hex.DecodeString(s)
		return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
	}
	if !validSHA(c.IntentSHA) || !validSHA(c.RootSHA) || !validSHA(d.Manifest.SHA256) || !validSHA(d.Derivative.SHA256) || d.Manifest.ArtifactID == uuid.Nil || d.Derivative.ArtifactID == uuid.Nil {
		return invalid()
	}
	m.Body = append([]byte(nil), m.Body...)
	m.Descriptor.ManifestBytes = append([]byte(nil), d.ManifestBytes...)
	sum := sha256.Sum256(m.Body)
	manifestSum := sha256.Sum256(m.Descriptor.ManifestBytes)
	if hex.EncodeToString(sum[:]) != d.Derivative.SHA256 || hex.EncodeToString(manifestSum[:]) != d.Manifest.SHA256 || !json.Valid(m.Descriptor.ManifestBytes) {
		return invalid()
	}
	normalized, err := canonical.Bytes(m.Descriptor.ManifestBytes)
	if err != nil || !bytes.Equal(normalized, m.Descriptor.ManifestBytes) || c.RootSHA != d.Manifest.SHA256 || c.Root != d.Manifest.ArtifactID {
		return invalid()
	}
	var binding struct {
		SchemaVersion  int         `json:"schema_version"`
		Request        uuid.UUID   `json:"request_id"`
		IntentSHA      string      `json:"intent_sha256"`
		Derivative     v2input.Ref `json:"derivative"`
		Bytes          int64       `json:"bytes"`
		MediaType      string      `json:"media_type"`
		ReviewRequired *bool       `json:"review_required"`
	}
	if json.Unmarshal(m.Descriptor.ManifestBytes, &binding) != nil || binding.SchemaVersion != 1 || binding.Request != d.Request || binding.IntentSHA != d.IntentSHA || binding.Derivative != d.Derivative || binding.Bytes != d.Bytes || binding.MediaType != d.MediaType || binding.ReviewRequired == nil || *binding.ReviewRequired != d.ReviewRequired {
		return invalid()
	}
	// Full codec/dimension/ICC verification belongs to the independent verifier;
	// this boundary additionally refuses a mislabeled wire format.
	switch d.MediaType {
	case "image/jpeg":
		if !bytes.HasPrefix(m.Body, []byte{0xff, 0xd8, 0xff}) {
			return invalid()
		}
	case "image/webp":
		if len(m.Body) < 12 || string(m.Body[:4]) != "RIFF" || string(m.Body[8:12]) != "WEBP" {
			return invalid()
		}
	default:
		return invalid()
	}
	return m, nil
}
