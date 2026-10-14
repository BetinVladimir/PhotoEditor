package workflow

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/review"
)

// CompileDeliveryExport reuses the encode compiler for one frozen WorkingImage.
// Caller must attest the source/recipe/current project ACL and nested pixel/ICC
// objects before publication/admission. This function creates no SQL records.
func CompileDeliveryExport(plan delivery.PreparePlan, parent uuid.UUID, child delivery.ExportChildIntent, imageRaw []byte, targetICCSHA string, registry Registry) (CompiledDownloadNode, error) {
	var zero CompiledDownloadNode
	request, _, err := plan.ExportRequest(parent, child)
	if err != nil {
		return zero, err
	}
	if len(imageRaw) < 2 || len(imageRaw) > 1<<20 || renderDerivativeBytesSHA(imageRaw) != request.Binding.Source.SHA256 {
		return zero, domain.NewError("DELIVERY_SOURCE_MISMATCH", 409)
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return zero, err
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != request.Binding.CanvasHash {
		return zero, domain.NewError("DELIVERY_SOURCE_MISMATCH", 409)
	}
	config, err := exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS), request.Preset.PresetSHA)
	if err != nil {
		return zero, err
	}
	// Encoding does not implement output sharpening. Never silently discard it.
	if config.Sharpen != nil {
		return zero, domain.NewError("DELIVERY_SHARPEN_UNAVAILABLE", 503)
	}
	// Metadata is applied only by the separately authorized metadata.write stage.
	// The immutable delivery request keeps its original policy and consent.
	config.MetadataPolicy = "strip"
	raw, err := canonical.Marshal(config)
	if err != nil {
		return zero, err
	}
	// Deliberately map only encoder fields; naming is handled by delivery packaging.
	var preset DownloadPreset
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return zero, domain.NewError("VALIDATION_FAILED", 422)
	}
	delete(fields, "naming_template")
	mapped, err := canonical.Marshal(fields)
	if err != nil || json.Unmarshal(mapped, &preset) != nil {
		return zero, domain.NewError("VALIDATION_FAILED", 422)
	}
	nodes, err := CompileDownloadExports(DownloadExportSpec{RunID: child.RunID, TargetICCSHA256: targetICCSHA, Preset: preset, Items: []DownloadExportItem{{Ordinal: 0, Manifest: review.ArtifactRef{ID: request.Binding.Source.ArtifactID, SHA256: request.Binding.Source.SHA256}, Image: image}}}, registry)
	if err != nil {
		return zero, err
	}
	if len(nodes) != 1 {
		return zero, domain.NewError("VALIDATION_FAILED", 422)
	}
	return nodes[0], nil
}
