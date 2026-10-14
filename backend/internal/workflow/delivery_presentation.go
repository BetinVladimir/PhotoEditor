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

// CompileDeliveryPresentation starts the first required derivative stage on
// the exact frozen source. Subsequent stages require accepted physical output
// refs; this compiler never invents future refs. Nil means encode can start.
func CompileDeliveryPresentation(plan delivery.PreparePlan, parent uuid.UUID, child delivery.ExportChildIntent, imageRaw []byte, registry Registry) (*CompiledDownloadNode, error) {
	request, _, err := plan.ExportRequest(parent, child)
	if err != nil {
		return nil, err
	}
	return compileDeliveryPresentationRequest(request,imageRaw,registry)
}

// Private value compiler; callers establish frozen request authority.
func compileDeliveryPresentationRequest(request delivery.FrozenExportRequest,imageRaw []byte,registry Registry)(*CompiledDownloadNode,error){
	if len(imageRaw) < 2 || len(imageRaw) > 1<<20 || renderDerivativeBytesSHA(imageRaw) != request.Binding.Source.SHA256 {
		return nil, domain.NewError("DELIVERY_SOURCE_MISMATCH", 409)
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return nil, err
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != request.Binding.CanvasHash || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" {
		return nil, domain.NewError("DELIVERY_SOURCE_MISMATCH", 409)
	}
	config, err := exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS), request.Preset.PresetSHA)
	if err != nil {
		return nil, err
	}
	ref := review.ArtifactRef{ID: request.Binding.Source.ArtifactID, SHA256: request.Binding.Source.SHA256}
	var operation string
	var params []byte
	if config.Size != nil {
		operation = "derivative.resize"
		fields := map[string]any{"image": ref, "fit": config.Size.Fit, "allow_upscale": config.Size.AllowUpscale}
		if config.Size.LongEdge != nil {
			fields["long_edge"] = *config.Size.LongEdge
		} else {
			fields["width"] = *config.Size.Width
			fields["height"] = *config.Size.Height
		}
		params, err = canonical.Marshal(fields)
	} else if config.Sharpen != nil {
		operation = "derivative.sharpen"
		params, err = canonical.Marshal(map[string]any{"image": ref, "radius": config.Sharpen.Radius, "strength": config.Sharpen.Strength, "threshold": config.Sharpen.Threshold, "max_delta": .03, "algorithm": "luma_unsharp_v1"})
	} else {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	op, ok := registry[operation]
	if !ok || op.Name != operation || op.Version != "1" || op.ResourceClass != "cpu" || op.Model != nil {
		return nil, domain.NewError("DELIVERY_PRESENTATION_UNAVAILABLE", 503)
	}
	cpu := false
	for _, backend := range op.Backends {
		cpu = cpu || backend == "cpu"
	}
	if !cpu {
		return nil, domain.NewError("DELIVERY_PRESENTATION_UNAVAILABLE", 503)
	}
	inputs := []review.ArtifactRef{ref, image.Pixels}
	if image.Alpha != nil {
		inputs = append(inputs, *image.Alpha)
	}
	seen := map[uuid.UUID]bool{}
	for _, input := range inputs {
		if !validExpansionRef(input) || seen[input.ID] {
			return nil, domain.NewError("DELIVERY_SOURCE_MISMATCH", 409)
		}
		seen[input.ID] = true
	}
	key := operation + "/0000000000000000"
	return &CompiledDownloadNode{ID: uuid.NewSHA1(request.ChildRunID, []byte(key)), Ordinal: 0, Key: key, Operation: operation, Version: op.Version, ResourceClass: op.ResourceClass, TimeoutSeconds: op.TimeoutSeconds, Inputs: inputs, Params: json.RawMessage(params)}, nil
}
