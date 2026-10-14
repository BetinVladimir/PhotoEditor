package workflow

import (
	"bytes"
	"encoding/json"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

// DeliveryEncodedImage matches the worker EncodedImage wire contract.
type DeliveryEncodedImage struct {
	Artifact       review.ArtifactRef `json:"artifact"`
	Format         string             `json:"format"`
	Width          int64              `json:"width"`
	Height         int64              `json:"height"`
	ICCID          string             `json:"icc_id"`
	ICCSHA256      string             `json:"icc_sha256"`
	Alpha          bool               `json:"alpha"`
	MetadataPolicy string             `json:"metadata_policy"`
}

// ValidateDeliveryEncodedManifest checks exact expected JSON, not raster bytes.
// Adoption must inspect the encoded object's physical SHA/header/ICC/metadata,
// producer fence, retention and current ACL before accepting an export as ready.
func ValidateDeliveryEncodedManifest(node CompiledDownloadNode, sourceRaw []byte, manifestRef review.ArtifactRef, manifestRaw []byte, encodedRef review.ArtifactRef) (DeliveryEncodedImage, error) {
	var zero DeliveryEncodedImage
	fail := func() (DeliveryEncodedImage, error) { return zero, domain.NewError("DELIVERY_ENCODED_MISMATCH", 409) }
	if !validExpansionRef(manifestRef) || !validExpansionRef(encodedRef) || manifestRef.ID == encodedRef.ID || len(manifestRaw) > 1<<20 || renderDerivativeBytesSHA(manifestRaw) != manifestRef.SHA256 {
		return fail()
	}
	for _, input := range node.Inputs {
		if input.ID == manifestRef.ID || input.ID == encodedRef.ID {
			return fail()
		}
	}
	// Reuse source/params/input validation; this does not reserve or dispatch RAM.
	const maximum = int64(9007199254740991)
	if _, err := CompileDeliveryEncodeProfile(node, sourceRaw, DeliveryDerivativeLimits{RAMBytes: maximum, ScratchBytes: maximum, OutputBytes: maximum}); err != nil {
		return fail()
	}
	image, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return fail()
	}
	var p encodeParams
	if json.Unmarshal(node.Params, &p) != nil {
		return fail()
	}
	w, h := image.Canvas.Width, image.Canvas.Height
	width, height := w, h
	if p.Size != nil {
		var scale float64
		if p.Size.LongEdge != nil {
			scale = float64(*p.Size.LongEdge) / float64(max(w, h))
		} else {
			scale = math.Min(float64(*p.Size.Width)/float64(w), float64(*p.Size.Height)/float64(h))
		}
		if !p.Size.AllowUpscale {
			scale = math.Min(1, scale)
		}
		width = max(int64(1), int64(math.RoundToEven(float64(w)*scale)))
		height = max(int64(1), int64(math.RoundToEven(float64(h)*scale)))
	}
	expected := DeliveryEncodedImage{Artifact: encodedRef, Format: p.Format, Width: width, Height: height, ICCID: p.ICCID, ICCSHA256: p.ICCSHA256, Alpha: image.Alpha != nil && p.Alpha == "preserve", MetadataPolicy: "strip"}
	exact, err := canonical.Marshal(expected)
	if err != nil || !bytes.Equal(exact, manifestRaw) {
		return fail()
	}
	return expected, nil
}
