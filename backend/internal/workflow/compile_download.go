package workflow

import (
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/encodecontract"
	"photoeditor/backend/internal/review"
)

type ExportSize = encodecontract.ExportSize
type DownloadPreset = encodecontract.DownloadPreset

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

type CompiledDownloadNode = encodecontract.CompiledDownloadNode
type encodeParams = encodecontract.Params

func validDownloadPreset(p DownloadPreset) bool { return encodecontract.ValidDownloadPreset(p) }

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
		width, height := encodeDimensions(item.Image.Canvas.Width, item.Image.Canvas.Height, spec.Preset.Size)
		if width < 1 || height < 1 || width > 200000000 || height > 200000000 || width*height > 200000000 {
			return fail()
		}
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
