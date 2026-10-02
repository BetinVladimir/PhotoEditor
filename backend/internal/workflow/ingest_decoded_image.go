package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

// DecodeOutputBinding must come from canonical asset.decode task outputs in the
// same tenant/run/ordinal. Ref validation alone cannot attest that provenance.
type DecodeOutputBinding struct {
	Manifest, Pixels review.ArtifactRef
	Alpha            *review.ArtifactRef
}

// ReadDecodedImage checks the exact manifest bytes and binds its nested refs to
// canonical outputs. Pixel bytes are validated by the producing worker; this
// function neither downloads full arrays nor substitutes for producer attestation.
func ReadDecodedImage(ctx context.Context, reader io.Reader, outputs DecodeOutputBinding, metadata IngestMetadata, workICCSHA string) (review.WorkingImage, error) {
	fail := func() (review.WorkingImage, error) {
		return review.WorkingImage{}, errors.New("invalid decoded image binding")
	}
	if reader == nil || !validExpansionRef(outputs.Manifest) || !validExpansionRef(outputs.Pixels) || !validExpansionRef(metadata.Source) || outputs.Manifest.ID == outputs.Pixels.ID || outputs.Manifest.ID == metadata.Source.ID || outputs.Pixels.ID == metadata.Source.ID {
		return fail()
	}
	if _, err := domain.ParseDigest(workICCSHA); err != nil {
		return fail()
	}
	if metadata.Width < 1 || metadata.Height < 1 || metadata.Width > 200000000 || metadata.Height > 200000000 || metadata.Width*metadata.Height > 200000000 || metadata.Orientation < 1 || metadata.Orientation > 8 {
		return fail()
	}
	switch metadata.Format {
	case "jpeg", "png", "tiff", "raw":
	default:
		return fail()
	}
	if metadata.HasAlpha != (outputs.Alpha != nil) {
		return fail()
	}
	if outputs.Alpha != nil && (!validExpansionRef(*outputs.Alpha) || outputs.Alpha.ID == outputs.Manifest.ID || outputs.Alpha.ID == outputs.Pixels.ID || outputs.Alpha.ID == metadata.Source.ID) {
		return fail()
	}
	if err := ctx.Err(); err != nil {
		return review.WorkingImage{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 1048577))
	if err != nil {
		return review.WorkingImage{}, err
	}
	if err = ctx.Err(); err != nil {
		return review.WorkingImage{}, err
	}
	hash := sha256.Sum256(raw)
	if len(raw) > 1048576 || hex.EncodeToString(hash[:]) != outputs.Manifest.SHA256 {
		return fail()
	}
	img, err := review.ParseWorkingImage(raw)
	if err != nil {
		return fail()
	}
	if img.Pixels != outputs.Pixels || (img.Alpha == nil) != (outputs.Alpha == nil) {
		return fail()
	}
	if img.Alpha != nil && *img.Alpha != *outputs.Alpha {
		return fail()
	}
	if img.DType != "float32" || img.Color.ProfileID != "work_rec2020_gamma24_v1" || img.Color.Encoding != "gamma24" || img.Color.ICCSHA256 != workICCSHA || img.Canvas.SourceSHA256 != metadata.Source.SHA256 || img.Canvas.SpaceID != "oriented:"+metadata.Source.SHA256 {
		return fail()
	}
	var chain struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if json.Unmarshal(img.TransformChain, &chain) != nil || len(chain.Steps) != 0 {
		return fail()
	}
	w, h := metadata.Width, metadata.Height
	if metadata.Format == "raw" {
		// inspect currently reports pre-LibRaw-orientation dimensions. Permit only
		// their exact pair (possibly swapped), not arbitrary equal-area canvases.
		if !((img.Canvas.Width == w && img.Canvas.Height == h) || (img.Canvas.Width == h && img.Canvas.Height == w)) {
			return fail()
		}
	} else {
		if metadata.Orientation >= 5 {
			w, h = h, w
		}
		if img.Canvas.Width != w || img.Canvas.Height != h {
			return fail()
		}
	}
	return img, nil
}
