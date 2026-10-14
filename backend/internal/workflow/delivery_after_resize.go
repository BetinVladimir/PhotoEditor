package workflow

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/review"
)

// CompileDeliveryAfterResize validates the expected first stage before building
// sharpening on its actual output. Nil means no sharpen is required. SQL must
// separately prove accepted producer/fence and current ACL for both documents.
func CompileDeliveryAfterResize(plan delivery.PreparePlan, parent uuid.UUID, child delivery.ExportChildIntent, sourceRaw []byte, outputRef review.ArtifactRef, outputRaw []byte, reportRef review.ArtifactRef, reportRaw []byte, registry Registry) (*CompiledDownloadNode, error) {
 request,_,err:=plan.ExportRequest(parent,child);if err!=nil{return nil,err}
 return compileDeliveryAfterResizeRequest(request,sourceRaw,outputRef,outputRaw,reportRef,reportRaw,registry)
}

func compileDeliveryAfterResizeRequest(request delivery.FrozenExportRequest,sourceRaw []byte,outputRef review.ArtifactRef,outputRaw []byte,reportRef review.ArtifactRef,reportRaw []byte,registry Registry)(*CompiledDownloadNode,error){
	first, err := compileDeliveryPresentationRequest(request, sourceRaw, registry)
	if err != nil {
		return nil, err
	}
	if first == nil || first.Operation != "derivative.resize" {
		return nil, domain.NewError("DELIVERY_STAGE_MISMATCH", 409)
	}
	image, err := ValidateDeliveryResizeResult(*first, sourceRaw, outputRef, outputRaw, reportRef, reportRaw)
	if err != nil {
		return nil, err
	}
	config, err := exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS), request.Preset.PresetSHA)
	if err != nil {
		return nil, err
	}
	if config.Sharpen == nil {
		return nil, nil
	}
	op, ok := registry["derivative.sharpen"]
	if !ok || op.Name != "derivative.sharpen" || op.Version != "1" || op.ResourceClass != "cpu" || op.Model != nil {
		return nil, domain.NewError("DELIVERY_PRESENTATION_UNAVAILABLE", 503)
	}
	cpu := false
	for _, backend := range op.Backends {
		cpu = cpu || backend == "cpu"
	}
	if !cpu {
		return nil, domain.NewError("DELIVERY_PRESENTATION_UNAVAILABLE", 503)
	}
	params, err := canonical.Marshal(map[string]any{"image": outputRef, "radius": config.Sharpen.Radius, "strength": config.Sharpen.Strength, "threshold": config.Sharpen.Threshold, "max_delta": .03, "algorithm": "luma_unsharp_v1"})
	if err != nil {
		return nil, err
	}
	inputs := []review.ArtifactRef{outputRef, image.Pixels}
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
	key := "derivative.sharpen/0000000000000000"
	return &CompiledDownloadNode{ID: uuid.NewSHA1(request.ChildRunID, []byte(key)), Ordinal: 0, Key: key, Operation: op.Name, Version: op.Version, ResourceClass: op.ResourceClass, TimeoutSeconds: op.TimeoutSeconds, Inputs: inputs, Params: params}, nil
}
