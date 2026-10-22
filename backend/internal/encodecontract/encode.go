// Package encodecontract owns pure export wire and resource validation.
// It deliberately has no workflow, delivery or provenance dependency.
package encodecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/review"
)

type ExportSize struct {
	LongEdge     *int64 `json:"long_edge,omitempty"`
	Width        *int64 `json:"width,omitempty"`
	Height       *int64 `json:"height,omitempty"`
	Fit          string `json:"fit"`
	AllowUpscale bool   `json:"allow_upscale"`
}

type DownloadPreset struct {
	NamingTemplate string      `json:"naming_template,omitempty"`
	Format         string      `json:"format"`
	ICCID          string      `json:"icc_id"`
	Size           *ExportSize `json:"size,omitempty"`
	Quality        *int        `json:"quality,omitempty"`
	Subsampling    string      `json:"subsampling"`
	Alpha          string      `json:"alpha"`
	MatteRGB       []float64   `json:"matte_rgb,omitempty"`
	MetadataPolicy string      `json:"metadata_policy"`
}

type CompiledDownloadNode struct {
	ID                                     uuid.UUID
	Ordinal                                int64
	Key, Operation, Version, ResourceClass string
	TimeoutSeconds                         int
	Inputs                                 []review.ArtifactRef
	Params                                 json.RawMessage
}

type Params struct {
	Image          review.ArtifactRef `json:"image"`
	Format         string             `json:"format"`
	ICCID          string             `json:"icc_id"`
	ICCSHA256      string             `json:"icc_sha256"`
	Size           *ExportSize        `json:"size,omitempty"`
	Quality        *int               `json:"quality,omitempty"`
	Subsampling    string             `json:"subsampling"`
	Alpha          string             `json:"alpha"`
	MatteRGB       []float64          `json:"matte_rgb,omitempty"`
	MetadataPolicy string             `json:"metadata_policy"`
}

func ValidDownloadPreset(p DownloadPreset) bool {
	if p.NamingTemplate != "" && !exportpreset.ValidNamingTemplate(p.NamingTemplate) {
		return false
	}
	if p.Format != "jpeg" && p.Format != "png" && p.Format != "tiff" {
		return false
	}
	if p.ICCID != "srgb_v1" && p.ICCID != "work_rec2020_gamma24_v1" || p.Subsampling != "444" ||
		(p.Alpha != "preserve" && p.Alpha != "matte") ||
		(p.Alpha == "matte") != (len(p.MatteRGB) == 3) || p.MetadataPolicy != "strip" {
		return false
	}
	for _, value := range p.MatteRGB {
		if value < 0 || value > 1 {
			return false
		}
	}
	if p.Format == "jpeg" {
		if p.Quality == nil || *p.Quality < 1 || *p.Quality > 100 || p.Alpha != "matte" {
			return false
		}
	} else if p.Quality != nil {
		return false
	}
	if p.Size != nil {
		s := p.Size
		long := s.LongEdge != nil
		box := s.Width != nil || s.Height != nil
		if long == box || (s.Width == nil) != (s.Height == nil) || s.Fit != "contain" {
			return false
		}
		for _, value := range []*int64{s.LongEdge, s.Width, s.Height} {
			if value != nil && (*value < 1 || *value > 100000) {
				return false
			}
		}
	}
	return true
}

type DeliveryDerivativeLimits struct{ RAMBytes, ScratchBytes, OutputBytes int64 }

// CompileDeliveryEncodeProfile matches export.memory.presentation_ram_bytes.
// Source refers to the last validated presentation output, not the original
// image when derivatives ran. Reserves downloaded NPY and encoded output bytes.
func CompileDeliveryEncodeProfile(node CompiledDownloadNode, sourceRaw []byte, limits DeliveryDerivativeLimits) (json.RawMessage, error) {
	fail := func() (json.RawMessage, error) { return nil, domain.NewError("DELIVERY_RESOURCE_INVALID", 422) }
	if node.Operation != "export.encode" || node.Version != "1" || node.ResourceClass != "cpu" || len(node.Inputs) < 2 || len(sourceRaw) > 1<<20 || renderDerivativeBytesSHA(sourceRaw) != node.Inputs[0].SHA256 {
		return fail()
	}
	image, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return fail()
	}
	if image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || node.Inputs[1] != image.Pixels || len(node.Inputs) != 2+boolInt(image.Alpha != nil) {
		return fail()
	}
	if image.Alpha != nil && node.Inputs[2] != *image.Alpha {
		return fail()
	}
	var p Params
	if json.Unmarshal(node.Params, &p) != nil || p.Image != node.Inputs[0] || invalidDigest(p.ICCSHA256) {
		return fail()
	}
	preset := DownloadPreset{Format: p.Format, ICCID: p.ICCID, Size: p.Size, Quality: p.Quality, Subsampling: p.Subsampling, Alpha: p.Alpha, MatteRGB: p.MatteRGB, MetadataPolicy: p.MetadataPolicy}
	if !ValidDownloadPreset(preset) {
		return fail()
	}
	w, h := image.Canvas.Width, image.Canvas.Height
	width, height := EncodeDimensions(w, h, p.Size)

	source, target := w*h, width*height
	if source < 1 || source > 200000000 || target < 1 || target > 200000000 {
		return fail()
	}
	sourceBytes := int64(12)
	if image.Alpha != nil {
		sourceBytes = 16
	}
	outputChannels := int64(6)
	if image.Alpha != nil && p.Alpha == "preserve" {
		outputChannels = 8
	}
	output := (8 << 20) + target*outputChannels
	estimate := ResourceEstimate{RAMBytes: EncodeRAMBytes(source, target, image.Alpha != nil), VRAMBytes: 0, ScratchBytes: (8 << 20) + source*sourceBytes + output, OutputBytes: output, PixelCount: target}
	if limits.RAMBytes < estimate.RAMBytes || limits.ScratchBytes < estimate.ScratchBytes || limits.OutputBytes < estimate.OutputBytes {
		return fail()
	}
	return canonical.Marshal(map[string]any{"backend": "cpu", "renderer_version": "v2.2", "resource_estimate": estimate})
}

// encodeDimensions matches export.encode._dimensions, including ties-to-even.
func EncodeDimensions(w, h int64, size *ExportSize) (int64, int64) {
	if size == nil {
		return w, h
	}
	var scale float64
	if size.LongEdge != nil {
		scale = float64(*size.LongEdge) / float64(max(w, h))
	} else {
		scale = math.Min(float64(*size.Width)/float64(w), float64(*size.Height)/float64(h))
	}
	if !size.AllowUpscale {
		scale = math.Min(1, scale)
	}
	return max(int64(1), int64(math.RoundToEven(float64(w)*scale))), max(int64(1), int64(math.RoundToEven(float64(h)*scale)))
}

// encodeRAMBytes matches export.memory.presentation_ram_bytes.
func EncodeRAMBytes(source, target int64, alpha bool) int64 {
	sourceRAM := int64(24)
	if alpha {
		sourceRAM = 96
	}
	return (128 << 20) + source*sourceRAM + target*96
}

type ResourceEstimate struct {
	RAMBytes     int64 `json:"ram_bytes"`
	VRAMBytes    int64 `json:"vram_bytes"`
	ScratchBytes int64 `json:"scratch_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
	PixelCount   int64 `json:"pixel_count"`
}

func renderDerivativeBytesSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func invalidDigest(raw string) bool { _, err := domain.ParseDigest(raw); return err != nil }
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
