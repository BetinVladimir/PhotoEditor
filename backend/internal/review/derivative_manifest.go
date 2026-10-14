package review

import (
	"bytes"
	"encoding/json"
	"math"

	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/objectstore"
)

type RenderDerivativeManifest struct {
	Source         ArtifactRef `json:"source"`
	Derivative     ArtifactRef `json:"derivative"`
	Variant        string      `json:"variant"`
	ProfileHash    string      `json:"profile_hash"`
	Width          int         `json:"width"`
	Height         int         `json:"height"`
	MediaType      string      `json:"media_type"`
	ColorProfile   string      `json:"color_profile"`
	MetadataPolicy string      `json:"metadata_policy"`
	Alpha          bool        `json:"alpha"`
}

// This validates authored manifest bytes and expected source geometry/binding.
// Adoption must separately verify producer/fence scope and encoded image bytes.
func ParseRenderDerivativeManifest(raw []byte, manifestRef, sourceRef, derivativeRef ArtifactRef, image WorkingImage, variant, srgbSHA string) (RenderDerivativeManifest, error) {
	fail := func() (RenderDerivativeManifest, error) { return RenderDerivativeManifest{}, objectstore.ErrIntegrity }
	if len(raw) < 2 || len(raw) > 1048576 || !manifestRef.valid() || !sourceRef.valid() || !derivativeRef.valid() || manifestRef.ID == sourceRef.ID || manifestRef.ID == derivativeRef.ID || sourceRef.ID == derivativeRef.ID || derivativeManifestSHA(raw) != manifestRef.SHA256 {
		return fail()
	}
	body, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(body, raw) {
		return fail()
	}
	var out RenderDerivativeManifest
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil {
		return fail()
	}
	roundtrip, err := canonical.Marshal(out)
	if err != nil || !bytes.Equal(roundtrip, body) {
		return fail()
	}
	profile, err := media.ProfileHash(variant, srgbSHA)
	if err != nil || (variant != "preview1600" && variant != "preview2560") || out.Variant != variant || out.ProfileHash != profile || out.Source != sourceRef || out.Derivative != derivativeRef || out.ColorProfile != "srgb" || out.MetadataPolicy != "stripped" {
		return fail()
	}
	w, h := image.Canvas.Width, image.Canvas.Height
	if w < 1 || h < 1 || w > 200000 || h > 200000 || int64(w)*int64(h) > 200000000 || image.DType != "float32" {
		return fail()
	}
	edge := 1600.
	if variant == "preview2560" {
		edge = 2560
	}
	scale := math.Min(1, edge/math.Max(float64(w), float64(h)))
	width := max(1, int(math.RoundToEven(float64(w)*scale)))
	height := max(1, int(math.RoundToEven(float64(h)*scale)))
	alpha := image.Alpha != nil
	mime := "image/jpeg"
	if alpha {
		mime = "image/webp"
	}
	if out.Width != width || out.Height != height || out.Alpha != alpha || out.MediaType != mime {
		return fail()
	}
	return out, nil
}

func derivativeManifestSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
