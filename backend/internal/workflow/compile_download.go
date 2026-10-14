package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
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
	Format         string      `json:"format"`
	ICCID          string      `json:"icc_id"`
	Size           *ExportSize `json:"size,omitempty"`
	Quality        *int        `json:"quality,omitempty"`
	Subsampling    string      `json:"subsampling"`
	Alpha          string      `json:"alpha"`
	MatteRGB       []float64   `json:"matte_rgb,omitempty"`
	MetadataPolicy string      `json:"metadata_policy"`
}

type DownloadExportItem struct {
	Ordinal  int64
	Manifest review.ArtifactRef
	Image    review.WorkingImage
}

type DownloadExportSpec struct {
	RunID           uuid.UUID
	TargetICCSHA256 string
	Preset          DownloadPreset
	Items           []DownloadExportItem
}

type CompiledDownloadNode struct {
	ID                                     uuid.UUID
	Ordinal                                int64
	Key, Operation, Version, ResourceClass string
	TimeoutSeconds                         int
	Inputs                                 []review.ArtifactRef
	Params                                 json.RawMessage
}

type encodeParams struct {
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

func validDownloadPreset(p DownloadPreset) bool {
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

// CompileDownloadExports creates stable encode nodes only. Callers must obtain
// Image from a hash-checked master manifest and prove every nested ref ready.
func CompileDownloadExports(spec DownloadExportSpec, registry Registry) ([]CompiledDownloadNode, error) {
	fail := func() ([]CompiledDownloadNode, error) {
		return nil, errors.New("invalid download export specification")
	}
	if spec.RunID == uuid.Nil || len(spec.Items) < 1 || len(spec.Items) > MaxDownloadItems || !validDownloadPreset(spec.Preset) {
		return fail()
	}
	if _, err := domain.ParseDigest(spec.TargetICCSHA256); err != nil {
		return fail()
	}
	op, ok := registry["export.encode"]
	if !ok || op.Name != "export.encode" || op.Version != "1" || op.ResourceClass != "cpu" || op.Model != nil {
		return fail()
	}
	cpu := false
	for _, backend := range op.Backends {
		cpu = cpu || backend == "cpu"
	}
	if !cpu {
		return fail()
	}
	items := append([]DownloadExportItem(nil), spec.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Ordinal < items[j].Ordinal })
	out := make([]CompiledDownloadNode, 0, len(items))
	for index, item := range items {
		if item.Ordinal != int64(index) || !validExpansionRef(item.Manifest) || !validExpansionRef(item.Image.Pixels) ||
			item.Manifest == item.Image.Pixels || item.Image.DType != "float32" ||
			item.Image.Color.ProfileID != "work_rec2020_gamma24_v1" || item.Image.Color.Encoding != "gamma24" ||
			item.Image.Canvas.Width < 1 || item.Image.Canvas.Height < 1 || item.Image.Canvas.Width > 200000000 || item.Image.Canvas.Height > 200000000 || item.Image.Canvas.Width*item.Image.Canvas.Height > 200000000 {
			return fail()
		}
  width,height := encodeDimensions(item.Image.Canvas.Width,item.Image.Canvas.Height,spec.Preset.Size)
  if width<1 || height<1 || width>200000000 || height>200000000 || width*height>200000000 { return fail() }
		inputs := []review.ArtifactRef{item.Manifest, item.Image.Pixels}
		if item.Image.Alpha != nil {
			if !validExpansionRef(*item.Image.Alpha) || *item.Image.Alpha == item.Manifest || *item.Image.Alpha == item.Image.Pixels {
				return fail()
			}
			inputs = append(inputs, *item.Image.Alpha)
		}
		params, err := canonical.Marshal(encodeParams{Image: item.Manifest, Format: spec.Preset.Format, ICCID: spec.Preset.ICCID,
			ICCSHA256: spec.TargetICCSHA256, Size: spec.Preset.Size, Quality: spec.Preset.Quality,
			Subsampling: spec.Preset.Subsampling, Alpha: spec.Preset.Alpha, MatteRGB: spec.Preset.MatteRGB, MetadataPolicy: "strip"})
		if err != nil || len(params) > 1048576 {
			return fail()
		}
		key := fmt.Sprintf("export.encode/%016d", item.Ordinal)
		out = append(out, CompiledDownloadNode{ID: uuid.NewSHA1(spec.RunID, []byte(key)), Ordinal: item.Ordinal, Key: key,
			Operation: op.Name, Version: op.Version, ResourceClass: op.ResourceClass, TimeoutSeconds: op.TimeoutSeconds,
			Inputs: inputs, Params: params})
	}
	return out, nil
}
