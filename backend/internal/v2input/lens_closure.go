package v2input

import (
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
)

type OpticalLensParams struct {
	SchemaVersion int                   `json:"schema_version"`
	Base          SessionVersionBinding `json:"base"`
	Strength      float64               `json:"strength"`
	Profile       Ref                   `json:"profile"`
	FocalMM       float64               `json:"focal_mm"`
	Aperture      float64               `json:"aperture"`
}

// CompileLensClosure freezes every readable lens dependency. The compiler must
// resolve profile/qualification and source children from hash-verified documents
// and obtain originalBinding via SQLLensOriginalSource.Load. No editor EXIF is used.
func CompileLensClosure(taskID uuid.UUID, params OpticalLensParams, paramsRef Ref,
	source CaptureImageInputs, geometry []Ref, qualification Ref,
	originalBinding LensOriginalBinding, originalBindingRef Ref) (CaptureClosure, error) {
	if taskID == uuid.Nil || params.SchemaVersion != 1 || params.Base.AssetVersionID == uuid.Nil ||
		!digestPattern.MatchString(params.Base.CanvasHash) || !finiteRange(params.Strength, 0, 1) ||
		!finiteRange(params.FocalMM, 1, 2000) || !finiteRange(params.Aperture, .5, 128) ||
		len(geometry) > 32 || originalBinding.Binding != params.Base {
		return CaptureClosure{}, ErrInvalid
	}
	allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".tif": true, ".tiff": true, ".dng": true, ".nef": true,
		".cr2": true, ".cr3": true, ".arw": true, ".raf": true, ".rw2": true, ".orf": true, ".pef": true}
	if !allowed[originalBinding.Suffix] {
		return CaptureClosure{}, ErrInvalid
	}
	bindingSHA, err := canonical.Hash(originalBinding)
	if err != nil || bindingSHA != originalBindingRef.SHA256 {
		return CaptureClosure{}, ErrInvalid
	}
	entries := []Entry{{paramsRef, "Params"}, {params.Base.Source, "WorkingImage"}, {params.Profile, "LensProfile"},
		{qualification, "QualificationReport"}, {originalBindingRef, "OriginalLensBinding"}, {originalBinding.Original, "OriginalImage"},
		{source.Pixels, "NpyArray"}}
	if source.Alpha != nil {
		entries = append(entries, Entry{*source.Alpha, "NpyArray"})
	}
	for _, ref := range geometry {
		entries = append(entries, Entry{ref, "NpyArray"})
	}
	return compileCaptureInventory(taskID, "optical.lens", params, paramsRef, entries)
}
func finiteRange(value, low, high float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= low && value <= high
}
