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

type DeliveryPresentationOutput struct {
	ImageRef   review.ArtifactRef
	ImageJSON  []byte
	ReportRef  review.ArtifactRef
	ReportJSON []byte
}

// CompileDeliveryFinalEncode replays the exact required presentation sequence.
// Payload checks do not prove adoption: caller must attest every result's task,
// fence, accepted state, physical arrays, retention and current ACL in SQL.
func CompileDeliveryFinalEncode(plan delivery.PreparePlan, parent uuid.UUID, child delivery.ExportChildIntent, sourceRaw []byte, results []DeliveryPresentationOutput, targetICC string, registry Registry) (CompiledDownloadNode, error) {
	var zero CompiledDownloadNode
	mismatch := func() (CompiledDownloadNode, error) { return zero, domain.NewError("DELIVERY_STAGE_MISMATCH", 409) }
	if len(results) > 2 {
		return mismatch()
	}
	first, err := CompileDeliveryPresentation(plan, parent, child, sourceRaw, registry)
	if err != nil {
		return zero, err
	}
	if first == nil {
		if len(results) != 0 {
			return mismatch()
		}
		return CompileDeliveryExport(plan, parent, child, sourceRaw, targetICC, registry)
	}
	if len(results) == 0 {
		return mismatch()
	}
	currentRaw := sourceRaw
	currentRef := first.Inputs[0]
	pending := first
	for index, result := range results {
		if pending == nil {
			return mismatch()
		}
		switch pending.Operation {
		case "derivative.resize":
			_, err = ValidateDeliveryResizeResult(*pending, currentRaw, result.ImageRef, result.ImageJSON, result.ReportRef, result.ReportJSON)
			if err != nil {
				return zero, err
			}
			pending, err = CompileDeliveryAfterResize(plan, parent, child, sourceRaw, result.ImageRef, result.ImageJSON, result.ReportRef, result.ReportJSON, registry)
			if err != nil {
				return zero, err
			}
		case "derivative.sharpen":
			_, err = ValidateDeliverySharpenResult(*pending, currentRaw, result.ImageRef, result.ImageJSON, result.ReportRef, result.ReportJSON)
			if err != nil {
				return zero, err
			}
			pending = nil
		default:
			return mismatch()
		}
		currentRaw = result.ImageJSON
		currentRef = result.ImageRef
		if index == len(results)-1 && pending != nil {
			return mismatch()
		}
	}
	request, _, err := plan.ExportRequest(parent, child)
	if err != nil {
		return zero, err
	}
	config, err := exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS), request.Preset.PresetSHA)
	if err != nil {
		return zero, err
	}
	// Geometry/sharpen have been verified above; encoding must not apply them twice.
	config.Size = nil
	config.Sharpen = nil
	// Metadata is applied only by the separately authorized metadata.write stage.
	// The immutable delivery request keeps its original policy and consent.
	config.MetadataPolicy = "strip"
	raw, err := canonical.Marshal(config)
	if err != nil {
		return zero, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return mismatch()
	}
	delete(fields, "naming_template")
	mapped, err := canonical.Marshal(fields)
	if err != nil {
		return zero, err
	}
	var preset DownloadPreset
	if json.Unmarshal(mapped, &preset) != nil {
		return mismatch()
	}
	image, err := review.ParseWorkingImage(currentRaw)
	if err != nil {
		return zero, err
	}
	nodes, err := CompileDownloadExports(DownloadExportSpec{RunID: child.RunID, TargetICCSHA256: targetICC, Preset: preset, Items: []DownloadExportItem{{Ordinal: 0, Manifest: currentRef, Image: image}}}, registry)
	if err != nil {
		return zero, err
	}
	if len(nodes) != 1 {
		return mismatch()
	}
	return nodes[0], nil
}
